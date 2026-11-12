package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/oekit/bootstrap"
	"github.com/oekit/concurrency/async"
	ekithertz "github.com/oekit/hertz"
	hertzmetrics "github.com/oekit/hertz/metrics"
	"github.com/oekit/logx"
	oekitmetrics "github.com/oekit/metrics"
	mw "github.com/oekit/middleware"
	"github.com/oekit/pkg/cli"

	"github.com/yi-nology/open-voice-bridge/app/doctor"
	"github.com/yi-nology/open-voice-bridge/app/runtime"
	apihandler "github.com/yi-nology/open-voice-bridge/biz/handler/api"
	"github.com/yi-nology/open-voice-bridge/biz/handler/ui"
	"github.com/yi-nology/open-voice-bridge/biz/service"
	"github.com/yi-nology/open-voice-bridge/internal/agent"
	"github.com/yi-nology/open-voice-bridge/internal/appconfig"
	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/audio"
	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/focus"
	"github.com/yi-nology/open-voice-bridge/internal/helper"
	"github.com/yi-nology/open-voice-bridge/internal/history"
	"github.com/yi-nology/open-voice-bridge/internal/input"
	"github.com/yi-nology/open-voice-bridge/internal/llm"
	"github.com/yi-nology/open-voice-bridge/internal/mcp"
	"github.com/yi-nology/open-voice-bridge/internal/phone"
	"github.com/yi-nology/open-voice-bridge/internal/remap"
	"github.com/yi-nology/open-voice-bridge/internal/text"
	"github.com/yi-nology/open-voice-bridge/web"
)

var version = "dev"

func main() {
	app := &cli.App{
		Name:    "xiaomi-remote",
		Version: version,
		Commands: []cli.Command{
			{
				Name:    "agent",
				Summary: "run the BLE/ATVV, ASR, and local UI agent",
				Run: func(_ context.Context, args []string, _, stderr io.Writer) int {
					return runAgent(args, stderr)
				},
			},
			{
				Name:    "remap",
				Summary: "run the root evdev/uinput key remapper",
				Run: func(_ context.Context, args []string, _, stderr io.Writer) int {
					return runRemap(args, stderr)
				},
			},
			{
				Name:    "doctor",
				Summary: "check config, services, devices, and ASR providers",
				Run: func(_ context.Context, args []string, stdout, stderr io.Writer) int {
					fs, path := flagSet("doctor", stderr)
					if _, code := cli.ParseFlags(fs, args); code != cli.ExitOK {
						return code
					}
					return doctor.Run(*path, newLogger("xiaomi-remote"), stdout)
				},
			},
			{
				Name:    "mcp",
				Summary: "serve one AI client over stdio MCP (read-only history/stats)",
				Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
					return runMCP(ctx, args, stdout, stderr)
				},
			},
		},
	}
	os.Exit(app.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func flagSet(name string, stderr io.Writer) (*flag.FlagSet, *string) {
	fs := cli.NewFlagSet(name, stderr)
	path := fs.String("config", config.DefaultPath(), "config file")
	return fs, path
}

func newLogger(service string) logx.Logger {
	logx.UseSlog(service)
	return logx.WithService(service)
}

// speakingLabel turns a microphone transition into the desktop pill's copy.
func speakingLabel(state input.AudioState, devices []input.Status) (bool, string, string) {
	if !state.Speaking || state.Owner == "" {
		return false, "", ""
	}
	if state.Owner == input.RemoteID {
		for _, device := range devices {
			if device.ID == input.RemoteID {
				return true, "remote", device.Name
			}
		}
		return true, "remote", "遥控器"
	}
	for _, device := range devices {
		if device.ID == state.Owner {
			return true, "phone", device.Name
		}
	}
	// An owner that dropped out of the list still counts as speaking; the pill
	// must not go quiet just because a label is missing.
	return true, "phone", state.Owner
}

func runAgent(args []string, stderr io.Writer) int {
	fs, path := flagSet("agent", stderr)
	if _, code := cli.ParseFlags(fs, args); code != cli.ExitOK {
		return code
	}
	logger := newLogger("xiaomi-remote")
	hertzLogger := logx.NewSlogLogger("hertz")
	hertzLogger.SetLevel(logx.LevelWarn)
	hlog.SetLogger(ekithertz.NewLogger(hertzLogger))
	cfg, err := appconfig.Load(*path)
	if err != nil {
		logger.Error("load config", "err", err)
		return 1
	}
	warnKeyPerms(logger, cfg.APIKeyFile)

	engine, buildErr := asr.Build(cfg.ASR)
	var asrErr error
	if buildErr != nil {
		asrErr = buildErr
		logger.Error("ASR unavailable; remapping and UI stay online", "err", buildErr)
		engine = asr.NewUnavailable(buildErr)
	}
	logger.Info("ASR chain", "providers", engine.Name(), "local", engine.Local())

	app, err := bootstrap.New(
		bootstrap.WithServiceName("xiaomi-remote"),
		bootstrap.WithShutdownTimeout(10*time.Second),
	)
	if err != nil {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if closeErr := engine.Close(closeCtx); closeErr != nil {
			logger.Warn("close ASR engine", "err", closeErr)
		}
		logger.Error("bootstrap", "err", err)
		return 1
	}
	err = app.Run(func(app *bootstrap.App) error {
		ctx := app.Context()
		sink, err := audio.New(audio.Config{SinkName: cfg.SinkName, SourceName: cfg.SourceName})
		if err != nil {
			if closeErr := engine.Close(ctx); closeErr != nil {
				logger.Warn("close ASR engine", "err", closeErr)
			}
			return err
		}
		if err := sink.Start(ctx); err != nil {
			if closeErr := sink.Close(ctx); closeErr != nil {
				logger.Warn("close audio sink", "err", closeErr)
			}
			if closeErr := engine.Close(ctx); closeErr != nil {
				logger.Warn("close ASR engine", "err", closeErr)
			}
			return err
		}
		ag := agent.New(cfg, engine, sink)
		inputHub := input.NewHub()
		ag.SetHub(inputHub)
		transcriptsTotal, asrLatency := runtime.RegisterAgentMetrics(ag, oekitmetrics.Registry)
		llmProcessor, llmErr := llm.Build(cfg.LLM)
		if llmErr != nil {
			logger.Error("LLM unavailable; continuing without post-processing", "err", llmErr)
		} else if llmProcessor != nil {
			logger.Info("LLM post-processor", "name", llmProcessor.Name())
		}
		ag.SetLLM(llmProcessor)
		textRules, textErr := text.New(cfg.Text)
		if textErr != nil {
			logger.Error("dictionary/snippets unavailable", "err", textErr)
		} else {
			ag.SetTextRules(textRules)
		}
		if err := ag.Start(ctx); err != nil {
			if closeErr := ag.Close(ctx); closeErr != nil {
				logger.Warn("close agent", "err", closeErr)
			}
			return err
		}
		// Common phrases ride the same synthetic-key channel as the voice key:
		// the watcher pastes into the focused window and announces to the UI.
		phraseWatcher := agent.NewPhraseWatcher(ag.Watcher(), ag.Speech())
		phraseWatcher.Start(ctx)
		app.AddResource("agent", func(closeCtx context.Context) error {
			return ag.Close(closeCtx)
		})

		helperClient := helper.New(cfg.HelperSocket)
		svc := service.New(cfg, *path, ag, helperClient, asrErr, logger)
		historyStore := history.New(filepath.Join(filepath.Dir(*path), "history.jsonl"), 200)
		statsStore := history.NewStats(filepath.Join(filepath.Dir(*path), "usage-stats.json"))

		phoneEnabled := cfg.Phone.Enabled
		var phoneStore *phone.Store
		var phoneServer *phone.Server
		if phoneEnabled {
			store, err := phone.NewStore(filepath.Join(filepath.Dir(*path), "phone_tokens.json"), phone.Config{
				PairCodeTTL: time.Duration(cfg.Phone.PairCodeTTLSeconds) * time.Second,
			})
			if err != nil {
				logger.Error("phone store unavailable; disabling phone input", "err", err)
				phoneEnabled = false
			} else {
				phoneStore = store
				phoneServer = phone.NewServer(store, phone.NewKeyInjector(helperClient), inputHub, phone.ServerConfig{
					Addr:                cfg.Phone.Addr,
					TLSMode:             cfg.Phone.TLS,
					CertDir:             filepath.Dir(*path),
					MaxClients:          cfg.Phone.MaxClients,
					MaxUtteranceSeconds: cfg.Phone.MaxUtteranceSeconds,
					Transcriber:         runtime.PhoneSpeech{Pipeline: ag.Speech()},
					Assets:              web.FS,
				})
			}
		}
		if phoneEnabled {
			app.GoBackground("phone", func(ctx context.Context) {
				if err := phoneServer.Serve(ctx); err != nil {
					logger.Error("phone server stopped", "err", err)
				}
			})
			logger.Info("phone input listener started", "addr", cfg.Phone.Addr, "tls", cfg.Phone.TLS)
		}

		phoneAdmin := runtime.PhoneAdmin{
			Enabled:    phoneEnabled,
			Configured: svc.PhoneEnabled,
			Store:      phoneStore,
			Server:     phoneServer,
			Hub:        inputHub,
			TLSMode:    cfg.Phone.TLS,
			Addr:       cfg.Phone.Addr,
		}

		deps := ui.Deps{
			Status:              svc.Status,
			Apply:               svc.Apply,
			Reconnect:           svc.Reconnect,
			RestartMic:          svc.RestartMic,
			SwitchASR:           svc.SwitchASR,
			KeymapProfiles:      svc.KeymapProfiles,
			ApplyKeymapProfile:  svc.ApplyKeymapProfile,
			SaveKeymapProfile:   svc.SaveKeymapProfile,
			DeleteKeymapProfile: svc.DeleteKeymapProfile,
			ASRConfig:           svc.ASRConfig,
			UpdateASRConfig:     svc.UpdateASRConfig,
			TestASR:             svc.TestASR,
			LLMConfig:           svc.LLMConfig,
			UpdateLLMConfig:     svc.UpdateLLMConfig,
			TestLLM:             svc.TestLLM,
			SaveLLMKey:          svc.SaveLLMKey,
			TextConfig:          svc.TextConfig,
			UpdateTextConfig:    svc.UpdateTextConfig,
			History:             func(context.Context) []history.Entry { return historyStore.List(200) },
			DeleteHistory:       func(_ context.Context, id string) error { return historyStore.Delete(id) },
			ClearHistory:        func(context.Context) error { return historyStore.Clear() },
			Stats:               func() history.UsageStats { return statsStore.Snapshot() },
			ExportConfig:        svc.ExportConfig,
			ImportConfig:        svc.ImportConfig,
			Diagnostics:         func(ctx context.Context) map[string]any { return svc.Diagnostics(ctx, version) },
			MCPVerify:           svc.MCPVerify,
			MCPAudit:            svc.MCPAudit,
			MCPCreateToken:      func(label string) (map[string]any, error) { return svc.MCPCreateToken(label, mcpBinaryPath()) },
			MCPListTokens:       svc.MCPListTokens,
			MCPRevokeToken:      svc.MCPRevokeToken,
			SnapshotState: func() map[string]any {
				owner := inputHub.AudioOwner()
				active, source, name := speakingLabel(
					input.AudioState{Speaking: owner != "", Owner: owner}, inputHub.Sources())
				return map[string]any{
					"listening": map[string]any{"active": active, "source": source, "source_name": name},
				}
			},
			Devices:          svc.Devices,
			DiscoverDevices:  svc.DiscoverDevices,
			SelectDevice:     svc.SelectDevice,
			SetDeviceEnabled: svc.SetDeviceEnabled,
			SaveDevice:       svc.SaveDevice,
			DeleteDevice:     svc.DeleteDevice,
			ConnectDevice:    svc.ConnectDevice,
			Ready:            svc.Ready,
			Inputs:           phoneAdmin.Inputs,
			PhonePairing:     phoneAdmin.Pairing,
			PhoneDevices:     phoneAdmin.Devices,
			PhoneRevoke:      phoneAdmin.Revoke,
			SetPhoneEnabled:  svc.SetPhoneEnabled,
			HelperSocket:     cfg.HelperSocket,
		}

		uiServer := ui.New(deps)
		// 切得准: follow the frontmost app's WM_CLASS into its keymap profile
		// at runtime (X11; the switcher stays idle where focus is not visible).
		if len(cfg.Keymap.AppRules) > 0 {
			async.GoSafe(func() {
				svc.StartAppSwitcher(ctx, 0, focus.ActiveClass)
			})
		}
		micOwner := inputHub.Watch()
		async.GoSafe(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case transcript := <-ag.Transcripts():
					transcriptsTotal.Inc()
					if transcript.Latency > 0 {
						asrLatency.Observe(transcript.Latency.Seconds())
					}
					if err := historyStore.Append(history.Entry{
						ID:         transcript.At.Format("20060102-150405.000000000"),
						Text:       transcript.Text,
						RawText:    transcript.RawText,
						Provider:   transcript.Provider,
						Language:   transcript.Language,
						Source:     transcript.Source,
						SourceName: transcript.SourceName,
						Address:    transcript.Address,
						Processed:  transcript.Processed,
						Truncated:  transcript.Truncated,
						LatencyMS:  transcript.Latency.Milliseconds(),
						At:         transcript.At,
					}); err != nil {
						logger.Warn("append history", "err", err)
					}
					statsStore.Record(transcript.Duration, len([]rune(transcript.Text)))
					uiServer.Publish(map[string]any{
						"event":       "transcript",
						"text":        transcript.Text,
						"raw_text":    transcript.RawText,
						"language":    transcript.Language,
						"provider":    transcript.Provider,
						"source":      transcript.Source,
						"source_name": transcript.SourceName,
						"address":     transcript.Address,
						"processed":   transcript.Processed,
						"truncated":   transcript.Truncated,
						"latency_ms":  transcript.Latency.Milliseconds(),
						"at":          transcript.At.Format(time.RFC3339),
					})
				case state := <-micOwner:
					active, source, name := speakingLabel(state, inputHub.Sources())
					uiServer.Publish(map[string]any{
						"event":       "listening",
						"active":      active,
						"source":      source,
						"source_name": name,
					})
				case phrase := <-phraseWatcher.Events():
					// Common phrases are user-authored text, not speech: they
					// show up live but never enter history or the log body.
					uiServer.Publish(map[string]any{
						"event": "phrase",
						"slot":  phrase.Slot,
						"text":  phrase.Text,
						"at":    phrase.At.Format(time.RFC3339),
					})
				}
			}
		})
		hertzServer := server.New(
			server.WithHostPorts(cfg.UIAddr),
			server.WithMaxRequestBodySize(1<<20),
			server.WithReadTimeout(10*time.Second),
			server.WithIdleTimeout(60*time.Second),
			server.WithExitWaitTime(5*time.Second),
			server.WithHandleMethodNotAllowed(true),
		)
		for _, middleware := range mw.DefaultMiddleware(mw.StackConfig{
			DisableResponseWrap: true,
			SkipPaths:           []string{"/", "/assets/*", "/healthz", "/readyz", "/metrics", "/events", "/api/status"},
			Prometheus: &mw.PrometheusConfig{
				Namespace:   "xiaomi_remote",
				ServiceName: "xiaomi-remote",
			},
			AccessLogOptions: []mw.AccessLogOption{mw.WithAccessLogLogger(logger)},
		}) {
			hertzServer.Use(middleware)
		}
		hertzServer.Use(uiServer.Security)
		hertzServer.Use(apihandler.WithBackend(uiServer))
		register(hertzServer, uiServer)
		hertzmetrics.InitAndRegister("xiaomi-remote", hertzServer)

		app.GoBackground("keymap-sync", svc.SyncKeymap)
		app.GoBackground("device-sync", svc.SyncDevices)
		ekithertz.RunServer(app, "ui", hertzServer, ekithertz.WithOnReady(func(context.Context) {
			app.Ready()
		}))
		logger.Info("xiaomi-remote agent running", "ui", "http://"+cfg.UIAddr, "config", *path)
		<-ctx.Done()
		return nil
	})
	if err != nil {
		logger.Error("agent stopped", "err", err)
		return 1
	}
	return 0
}

func runRemap(args []string, stderr io.Writer) int {
	fs := cli.NewFlagSet("remap", stderr)
	mapping := fs.String("mapping", "/etc/xiaomi-remote/mapping.json", "mapping file")
	socket := fs.String("socket", "/run/xiaomi-remote-helper.sock", "Unix socket")
	group := fs.Int("group", 1000, "socket group id")
	agentUID := fs.Int("agent-uid", 0, "uid allowed to run privileged commands besides root (0 = root only)")
	if _, code := cli.ParseFlags(fs, args); code != cli.ExitOK {
		return code
	}
	logger := newLogger("xiaomi-remote-remap")
	if os.Geteuid() != 0 {
		logger.Error("remap requires root for /dev/input and /dev/uinput")
		return 1
	}
	daemon := remap.New(remap.Config{
		MappingPath: *mapping,
		SocketPath:  *socket,
		SocketGroup: *group,
		AgentUID:    *agentUID,
	})
	app, err := bootstrap.New(
		bootstrap.WithServiceName("xiaomi-remote-remap"),
		bootstrap.WithShutdownTimeout(5*time.Second),
	)
	if err != nil {
		logger.Error("bootstrap", "err", err)
		return 1
	}
	err = app.Run(func(app *bootstrap.App) error {
		app.AddResource("remap", func(ctx context.Context) error {
			return daemon.Close(ctx)
		})
		app.GoBackground("remap", func(ctx context.Context) {
			if err := daemon.Run(ctx); err != nil {
				logger.Error("remapper stopped", "err", err)
				app.RequestShutdown()
			}
		})
		app.Ready()
		<-app.Context().Done()
		return nil
	})
	if err != nil {
		logger.Error("remapper stopped", "err", err)
		return 1
	}
	return 0
}

// mcpBinaryPath is the absolute path AI clients put in their MCP config.
func mcpBinaryPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "xiaomi-remote"
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved
	}
	return exe
}

// runMCP serves one AI client over stdio MCP: read-only history and stats
// behind a per-client token ("本地 Agent 访问").
func runMCP(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := cli.NewFlagSet("mcp", stderr)
	token := fs.String("token", os.Getenv("XIAOMI_REMOTE_TOKEN"), "client token (or XIAOMI_REMOTE_TOKEN)")
	base := fs.String("base", "http://127.0.0.1:8787", "admin API base URL")
	if _, code := cli.ParseFlags(fs, args); code != cli.ExitOK {
		return code
	}
	if strings.TrimSpace(*token) == "" {
		fmt.Fprintln(stderr, "mcp: a token is required — create one in the admin UI under 本地 Agent 访问")
		return 1
	}
	server := mcp.NewServer(*base, *token, nil)
	label, err := server.Verify(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "mcp: token rejected:", err)
		return 1
	}
	fmt.Fprintln(stderr, "mcp: serving", label, "(read-only)")
	if err := server.Serve(ctx, os.Stdin, stdout); err != nil {
		fmt.Fprintln(stderr, "mcp:", err)
		return 1
	}
	return 0
}

func warnKeyPerms(logger logx.Logger, path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		logger.Warn("MiMo API key not readable", "path", path, "err", err)
		return
	}
	if info.Mode().Perm()&0o077 != 0 {
		logger.Warn("MiMo API key is readable by group/other; chmod 600 recommended", "path", path)
	}
}
