package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/oekit/bootstrap"
	"github.com/oekit/concurrency/async"
	ekithertz "github.com/oekit/hertz"
	"github.com/oekit/logx"
	oekitmetrics "github.com/oekit/metrics"

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
	"github.com/yi-nology/open-voice-bridge/internal/phone"
	"github.com/yi-nology/open-voice-bridge/internal/text"
	"github.com/yi-nology/open-voice-bridge/web"
)

// AgentOptions is what the agent command needs from the CLI layer: the parsed
// config path, the build version, a logger, and the generated route table.
type AgentOptions struct {
	ConfigPath string
	Version    string
	Logger     logx.Logger
	Register   func(*server.Hertz, *ui.Server)
}

// RunAgent starts every subsystem and blocks until the process is asked to
// stop. A subsystem that cannot start is logged and degraded, not fatal: the
// warnUnknownKeys surfaces config keys the loader would silently drop —
// a typo'd option looks saved but does nothing (OPS-10).
func warnUnknownKeys(logger logx.Logger, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	if unknown := config.LintUnknown(data); len(unknown) > 0 {
		logger.Warn("unknown config keys are ignored", "keys", strings.Join(unknown, ", "), "path", path)
	}
}

// remapper and the admin UI stay online so the user can fix the cause.
func RunAgent(opts AgentOptions) int {
	logger := opts.Logger
	hertzLogger := logx.NewSlogLogger("hertz")
	hertzLogger.SetLevel(logx.LevelWarn)
	hlog.SetLogger(ekithertz.NewLogger(hertzLogger))
	cfg, err := appconfig.Load(opts.ConfigPath)
	if err != nil {
		logger.Error("load config", "err", err)
		return 1
	}
	warnKeyPerms(logger, cfg.APIKeyFile)
	warnUnknownKeys(logger, opts.ConfigPath)

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
		transcriptsTotal, asrLatency := RegisterAgentMetrics(ag, oekitmetrics.Registry)
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
		seqWatcher := agent.NewSeqActionWatcher(ag.Watcher(), ag.SeqAction)
		seqWatcher.Start(ctx)
		ag.SetActiveClassFunc(focus.ActiveClass)
		ag.SetRewriteRules(cfg.LLM.AppRules)
		ag.SetStreamingSubtitles(cfg.StreamingSubtitles)
		if cfg.WakeEnabled {
			ag.SetWake(true, cfg.WakeWord)
		}
		if cfg.LookbackEnabled {
			ag.SetLookback(true, cfg.LookbackSeconds)
		}
		app.AddResource("agent", func(closeCtx context.Context) error {
			return ag.Close(closeCtx)
		})

		helperClient := helper.New(cfg.HelperSocket)
		svc := service.New(cfg, opts.ConfigPath, ag, helperClient, asrErr, logger)
		historyStore := history.New(filepath.Join(filepath.Dir(opts.ConfigPath), "history.jsonl"), 200)
		statsStore := history.NewStats(filepath.Join(filepath.Dir(opts.ConfigPath), "usage-stats.json"))

		phoneEnabled := cfg.Phone.Enabled
		var phoneStore *phone.Store
		var phoneServer *phone.Server
		if phoneEnabled {
			store, err := phone.NewStore(filepath.Join(filepath.Dir(opts.ConfigPath), "phone_tokens.json"), phone.Config{
				PairCodeTTL: time.Duration(cfg.Phone.PairCodeTTLSeconds) * time.Second,
			})
			if err != nil {
				logger.Error("phone store unavailable; disabling phone input", "err", err)
				phoneEnabled = false
			} else {
				phoneStore = store
				phoneServer = phone.NewServer(store, phone.NewKeyInjector(helperClient, cfg.Phone.AllowPowerKey), inputHub, phone.ServerConfig{
					Addr:                cfg.Phone.Addr,
					TLSMode:             cfg.Phone.TLS,
					CertDir:             filepath.Dir(opts.ConfigPath),
					MaxClients:          cfg.Phone.MaxClients,
					MaxUtteranceSeconds: cfg.Phone.MaxUtteranceSeconds,
					Transcriber:         PhoneSpeech{Pipeline: ag.Speech()},
					Assets:              web.FS,
				})
			}
		}
		if phoneEnabled {
			listener, live := listenPhone(logger, phoneServer, cfg.Phone.TLS)
			if !live {
				phoneEnabled = false
			} else {
				app.GoBackground("phone", func(ctx context.Context) {
					if err := phoneServer.Serve(ctx, listener); err != nil {
						logger.Error("phone server stopped", "err", err)
					}
				})
			}
		}

		admin := Admin{
			Service: svc,
			History: historyStore,
			Stats:   statsStore,
			Hub:     inputHub,
			Phone: PhoneAdmin{
				Enabled:    phoneEnabled,
				Configured: svc.PhoneEnabled,
				Store:      phoneStore,
				Server:     phoneServer,
				Hub:        inputHub,
				TLSMode:    cfg.Phone.TLS,
				Addr:       cfg.Phone.Addr,
			},
			Addr:          cfg.UIAddr,
			HelperSocket:  cfg.HelperSocket,
			Version:       opts.Version,
			MCPBinaryPath: mcpBinaryPath,
			Logger:        logger,
			Register:      opts.Register,
		}
		uiServer := admin.UI()
		// 切得准: follow the frontmost app's WM_CLASS into its keymap profile
		// at runtime (X11; the switcher stays idle where focus is not visible).
		if len(cfg.Keymap.AppRules) > 0 {
			async.GoSafe(func() {
				svc.StartAppSwitcher(ctx, 0, focus.ActiveClass)
			})
		}
		events := Events{
			Transcripts:      ag.Transcripts(),
			Microphone:       inputHub.Watch(),
			Phrases:          phraseWatcher.Events(),
			Pickers:          phraseWatcher.Pickers(),
			Sources:          inputHub.Sources,
			History:          historyStore,
			Usage:            statsStore,
			Publish:          uiServer.Publish,
			TranscriptsTotal: transcriptsTotal,
			ASRLatency:       asrLatency,
			Logger:           logger,
		}
		async.GoSafe(func() { events.Run(ctx) })
		hertzServer := admin.HTTP(uiServer)

		app.GoBackground("keymap-sync", svc.SyncKeymap)
		app.GoBackground("device-sync", svc.SyncDevices)
		ekithertz.RunServer(app, "ui", hertzServer, ekithertz.WithOnReady(func(context.Context) {
			app.Ready()
		}))
		logger.Info("xiaomi-remote agent running", "ui", "http://"+cfg.UIAddr, "config", opts.ConfigPath)
		<-ctx.Done()
		return nil
	})
	if err != nil {
		logger.Error("agent stopped", "err", err)
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
