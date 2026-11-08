package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
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
	"github.com/prometheus/client_golang/prometheus"

	apihandler "github.com/yi-nology/open-voice-bridge/biz/handler/api"
	"github.com/yi-nology/open-voice-bridge/biz/handler/ui"
	"github.com/yi-nology/open-voice-bridge/biz/service"
	"github.com/yi-nology/open-voice-bridge/internal/agent"
	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/audio"
	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/helper"
	"github.com/yi-nology/open-voice-bridge/internal/history"
	"github.com/yi-nology/open-voice-bridge/internal/llm"
	"github.com/yi-nology/open-voice-bridge/internal/remap"
	"github.com/yi-nology/open-voice-bridge/internal/text"
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
					return runDoctor(args, stdout, stderr)
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

func registerAgentMetrics(ag *agent.Agent) (prometheus.Counter, prometheus.Histogram) {
	transcriptsTotal := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "xiaomi_remote_transcripts_total",
		Help: "Total number of recognized transcripts.",
	})
	asrLatency := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "xiaomi_remote_asr_duration_seconds",
		Help:    "ASR latency per transcript.",
		Buckets: []float64{0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
	})
	oekitmetrics.Registry.MustRegister(
		transcriptsTotal,
		asrLatency,
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "xiaomi_remote_ble_connected",
			Help: "Whether the remote is connected over BLE (1/0).",
		}, func() float64 { return boolGauge(ag, "connected") }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "xiaomi_remote_atvv_ready",
			Help: "Whether the ATVV voice bridge is ready (1/0).",
		}, func() float64 { return boolGauge(ag, "ready") }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "xiaomi_remote_mic_healthy",
			Help: "Whether the virtual microphone node is present (1/0).",
		}, func() float64 { return boolGauge(ag, "mic_healthy") }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "xiaomi_remote_last_asr_latency_seconds",
			Help: "Latency of the most recent transcription in seconds.",
		}, func() float64 {
			st := ag.Status()
			ms, _ := st["last_latency_ms"].(int64)
			return float64(ms) / 1000
		}),
	)
	return transcriptsTotal, asrLatency
}

func boolGauge(ag *agent.Agent, key string) float64 {
	if v, _ := ag.Status()[key].(bool); v {
		return 1
	}
	return 0
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
	cfg, err := config.Load(*path)
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
		transcriptsTotal, asrLatency := registerAgentMetrics(ag)
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
		app.AddResource("agent", func(closeCtx context.Context) error {
			return ag.Close(closeCtx)
		})

		helperClient := helper.New(cfg.HelperSocket)
		svc := service.New(cfg, *path, ag, helperClient, asrErr, logger)
		historyStore := history.New(filepath.Join(filepath.Dir(*path), "history.jsonl"), 200)
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
			Devices:             svc.Devices,
			DiscoverDevices:     svc.DiscoverDevices,
			SelectDevice:        svc.SelectDevice,
			SetDeviceEnabled:    svc.SetDeviceEnabled,
			SaveDevice:          svc.SaveDevice,
			DeleteDevice:        svc.DeleteDevice,
			ConnectDevice:       svc.ConnectDevice,
			Ready:               svc.Ready,
			HelperSocket:        cfg.HelperSocket,
		}

		uiServer := ui.New(deps)
		customUI = uiServer
		apihandler.SetBackend(uiServer)
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
						ID:        transcript.At.Format("20060102-150405.000000000"),
						Text:      transcript.Text,
						RawText:   transcript.RawText,
						Provider:  transcript.Provider,
						Language:  transcript.Language,
						Address:   transcript.Address,
						Processed: transcript.Processed,
						LatencyMS: transcript.Latency.Milliseconds(),
						At:        transcript.At,
					}); err != nil {
						logger.Warn("append history", "err", err)
					}
					uiServer.Publish(map[string]any{
						"event":      "transcript",
						"text":       transcript.Text,
						"raw_text":   transcript.RawText,
						"language":   transcript.Language,
						"provider":   transcript.Provider,
						"address":    transcript.Address,
						"processed":  transcript.Processed,
						"latency_ms": transcript.Latency.Milliseconds(),
						"at":         transcript.At.Format(time.RFC3339),
					})
				case active := <-ag.Listening():
					uiServer.Publish(map[string]any{"event": "listening", "active": active})
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
		register(hertzServer)
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

func runDoctor(args []string, stdout, stderr io.Writer) int {
	fs, path := flagSet("doctor", stderr)
	if _, code := cli.ParseFlags(fs, args); code != cli.ExitOK {
		return code
	}
	logger := newLogger("xiaomi-remote")
	cfg, err := config.Load(*path)
	if err != nil {
		logger.Error("load config", "err", err)
		return 1
	}
	fmt.Fprintln(stdout, "config:", *path)
	fmt.Fprintln(stdout, "address:", cfg.Address)
	fmt.Fprintln(stdout, "ui_addr:", cfg.UIAddr)
	fmt.Fprintln(stdout, "helper_socket:", cfg.HelperSocket)

	failures := 0
	for _, diagnostic := range asr.Diagnose(cfg.ASR) {
		if diagnostic.OK {
			fmt.Fprintf(stdout, "ok: asr/%s: %s\n", diagnostic.Provider, diagnostic.Detail)
		} else {
			fmt.Fprintf(stdout, "FAIL: asr/%s: %s\n", diagnostic.Provider, diagnostic.Detail)
			failures++
		}
	}
	if info, err := os.Stat(cfg.APIKeyFile); err == nil {
		perm := info.Mode().Perm()
		if perm&0o077 != 0 {
			fmt.Fprintln(stdout, "WARN: legacy api_key_file is readable by group/other; chmod 600 recommended")
		}
	}

	primary := cfg.ASR.Providers[cfg.ASR.Provider]
	providerType := strings.ToLower(primary.Type)
	if providerType == "" && cfg.ASR.Provider == "mimo" {
		providerType = "mimo"
	}
	if providerType == "mimo" || providerType == "openai" {
		keyFile := expandHome(primary.APIKeyFile)
		if keyFile == "" {
			fmt.Fprintln(stdout, "FAIL: api_key_file missing for provider", cfg.ASR.Provider)
			failures++
		} else if info, err := os.Stat(keyFile); err != nil {
			fmt.Fprintf(stdout, "FAIL: API key %s: %v\n", keyFile, err)
			failures++
		} else {
			perm := info.Mode().Perm()
			fmt.Fprintf(stdout, "ok: API key %s (%d bytes, %04o)\n", keyFile, info.Size(), perm)
			if perm&0o077 != 0 {
				fmt.Fprintln(stdout, "WARN: API key is readable by group/other; chmod 600 recommended")
			}
		}
	}
	doctorCtx, cancelDoctor := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelDoctor()
	if helperStatus, err := helper.New(cfg.HelperSocket).Call(doctorCtx, map[string]string{"cmd": "status"}); err != nil {
		fmt.Fprintln(stdout, "FAIL: remap helper:", err)
		failures++
	} else {
		fmt.Fprintf(stdout, "ok: remap helper (active=%v, remote_present=%v)\n",
			helperStatus["keyd_active"], helperStatus["remote_present"])
	}
	for _, binary := range []string{"pw-cat", "pw-loopback", "xsel", "bluetoothctl"} {
		if _, err := exec.LookPath(binary); err != nil {
			fmt.Fprintln(stdout, "FAIL: missing", binary)
			failures++
		} else {
			fmt.Fprintln(stdout, "ok:", binary)
		}
	}
	for _, unit := range []struct {
		label string
		args  []string
	}{
		{"agent active", []string{"--user", "is-active", "xiaomi-remote-go"}},
		{"agent autostart", []string{"--user", "is-enabled", "xiaomi-remote-go"}},
		{"remap active", []string{"is-active", "xiaomi-remote-remap"}},
		{"remap autostart", []string{"is-enabled", "xiaomi-remote-remap"}},
	} {
		out, err := exec.Command("systemctl", unit.args...).Output()
		state := strings.TrimSpace(string(out))
		if err != nil {
			fmt.Fprintf(stdout, "WARN: %s: %s\n", unit.label, state)
			continue
		}
		fmt.Fprintf(stdout, "ok: %s: %s\n", unit.label, state)
	}
	if current, err := user.Current(); err == nil {
		out, err := exec.Command("loginctl", "show-user", current.Username, "-p", "Linger").Output()
		if err == nil && strings.Contains(string(out), "Linger=yes") {
			fmt.Fprintln(stdout, "ok: linger enabled (starts before login)")
		} else {
			fmt.Fprintln(stdout, "WARN: linger disabled; agent will not start until you log in")
		}
	}
	if info, err := os.Stat("/dev/uinput"); err != nil {
		fmt.Fprintln(stdout, "WARN: /dev/uinput missing:", err)
	} else {
		fmt.Fprintf(stdout, "ok: /dev/uinput (%s)\n", info.Mode())
	}
	if os.Getenv("DISPLAY") == "" {
		fmt.Fprintln(stdout, "WARN: DISPLAY is empty; text injection will fail")
	} else {
		fmt.Fprintln(stdout, "ok: DISPLAY", os.Getenv("DISPLAY"))
	}
	if hasRemote() {
		fmt.Fprintln(stdout, "ok: RC003 evdev device present")
	} else {
		fmt.Fprintln(stdout, "WARN: RC003 evdev device not found (bluetoothctl connect?)")
	}
	if failures > 0 {
		return 1
	}
	return 0
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
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

func hasRemote() bool {
	return remap.RemotePresent()
}
