package runtime

import (
	"context"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	hertzmetrics "github.com/oekit/hertz/metrics"
	"github.com/oekit/logx"
	mw "github.com/oekit/middleware"

	apihandler "github.com/yi-nology/open-voice-bridge/biz/handler/api"
	"github.com/yi-nology/open-voice-bridge/biz/handler/ui"
	"github.com/yi-nology/open-voice-bridge/biz/service"
	"github.com/yi-nology/open-voice-bridge/internal/core"
	"github.com/yi-nology/open-voice-bridge/internal/history"
	"github.com/yi-nology/open-voice-bridge/internal/input"
)

// Admin assembles the loopback admin edge: the payloads the desktop calls and
// the engine that serves them. Register is injected rather than owned here
// because hz generates the route table into package main at the repo root.
type Admin struct {
	Service       *service.Service
	History       *history.Store
	Stats         *history.Stats
	Hub           *input.Hub
	Phone         PhoneAdmin
	Addr          string
	HelperSocket  string
	Version       string
	MCPBinaryPath func() string
	Logger        logx.Logger
	Register      func(*server.Hertz, *ui.Server)
}

// UI builds the payload server. It is separate from HTTP so the caller can
// subscribe to its event feed before the engine starts accepting requests.
func (a Admin) UI() *ui.Server { return ui.New(a.deps()) }

// HTTP builds the engine: middleware stack, same-origin security, backend
// injection, routes and the Hertz collectors.
func (a Admin) HTTP(uiServer *ui.Server) *server.Hertz {
	hertzServer := server.New(
		server.WithHostPorts(a.Addr),
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
			Namespace:   "xiaoyao",
			ServiceName: "xiaoyao",
		},
		AccessLogOptions: []mw.AccessLogOption{mw.WithAccessLogLogger(a.Logger)},
	}) {
		hertzServer.Use(middleware)
	}
	hertzServer.Use(uiServer.Security)
	hertzServer.Use(apihandler.WithBackend(uiServer))
	a.Register(hertzServer, uiServer)
	hertzmetrics.InitAndRegister("xiaoyao", hertzServer)
	return hertzServer
}

func (a Admin) deps() ui.Deps {
	svc := a.Service
	return ui.Deps{
		Status:              func(ctx context.Context) core.StatusView { return BuildStatus(ctx, svc.ViewInputs()) },
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
		History:             func(context.Context) []history.Entry { return a.History.List(200) },
		DeleteHistory:       func(_ context.Context, id string) error { return a.History.Delete(id) },
		ClearHistory:        func(context.Context) error { return a.History.Clear() },
		Stats:               func() history.UsageStats { return a.Stats.Snapshot() },
		ExportConfig:        svc.ExportConfig,
		ImportConfig:        svc.ImportConfig,
		Diagnostics: func(ctx context.Context) core.DiagnosticsView {
			return BuildDiagnostics(ctx, svc.ViewInputs(), a.Version)
		},
		MCPVerify:      svc.MCPVerify,
		MCPAudit:       svc.MCPAudit,
		MCPCreateToken: func(label string) (core.MCPTokenView, error) { return svc.MCPCreateToken(label, a.MCPBinaryPath()) },
		MCPListTokens:  svc.MCPListTokens,
		MCPRevokeToken: svc.MCPRevokeToken,
		SnapshotState: func() core.ListeningState {
			owner := a.Hub.AudioOwner()
			active, source, name := SpeakingLabel(
				input.AudioState{Speaking: owner != "", Owner: owner}, a.Hub.Sources())
			return core.ListeningState{Active: active, Source: source, SourceName: name}
		},
		Devices:               func(ctx context.Context) core.DevicesView { return BuildDevices(ctx, svc.ViewInputs()) },
		DiscoverDevices:       func(ctx context.Context) core.DiscoveredView { return BuildDiscovered(ctx, svc.ViewInputs()) },
		Permissions:           func(ctx context.Context) core.PermissionsView { return BuildPermissions(ctx, svc.ViewInputs()) },
		SelectDevice:          svc.SelectDevice,
		SetDeviceEnabled:      svc.SetDeviceEnabled,
		SaveDevice:            svc.SaveDevice,
		DeleteDevice:          svc.DeleteDevice,
		ConnectDevice:         svc.ConnectDevice,
		Ready:                 func(ctx context.Context) core.ReadyView { return BuildReady(ctx, svc.ViewInputs()) },
		Inputs:                a.Phone.Inputs,
		PhonePairing:          a.Phone.Pairing,
		PhoneDevices:          a.Phone.Devices,
		PhoneRevoke:           a.Phone.Revoke,
		TextCommands:          svc.TextCommands,
		UpdateTextCommands:    svc.UpdateTextCommands,
		PastePhrase:           svc.PastePhrase,
		LLMAppRules:           svc.LLMAppRules,
		UpdateLLMAppRules:     svc.UpdateLLMAppRules,
		StreamingSubtitles:    svc.StreamingSubtitles,
		SetStreamingSubtitles: svc.SetStreamingSubtitles,
		PasteConfig:           svc.PasteConfig,
		SetPasteConfig:        svc.SetPasteConfig,
		WakeConfig:            svc.WakeConfig,
		SetWake:               svc.SetWake,
		LookbackConfig:        svc.LookbackConfig,
		SetLookback:           svc.SetLookback,
		RecoverLookback:       svc.RecoverLookback,
		HistoryPolicy:         svc.HistoryPolicy,
		SetHistoryPolicy: func(retentionDays int, redactAddresses bool) error {
			if err := svc.SetHistoryPolicy(retentionDays, redactAddresses); err != nil {
				return err
			}
			// The store holds the running bounds; the service owns the saved
			// config. Both must move together or the control lies (波 3).
			a.History.SetPolicy(history.Policy{RetentionDays: retentionDays, RedactAddresses: redactAddresses})
			return nil
		},
		SetPhoneEnabled:  svc.SetPhoneEnabled,
		PhonePowerKey:    svc.PhonePowerKey,
		SetPhonePowerKey: svc.SetPhonePowerKey,
		HelperSocket:     a.HelperSocket,
	}
}
