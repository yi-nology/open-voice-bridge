package runtime

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/redact"

	"github.com/yi-nology/open-voice-bridge/biz/service"
	"github.com/yi-nology/open-voice-bridge/internal/bluez"
	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/core"
	"github.com/yi-nology/open-voice-bridge/internal/device"
	"github.com/yi-nology/open-voice-bridge/internal/helper"
)

// The typed view builders live here (ARC-09/ARC-05): app/runtime is the
// composition root and the only place that turns the service's state, the
// helper's IPC maps and the OS probes into the views the edge serialises.

func BuildStatus(ctx context.Context, in service.ViewInputs) core.StatusView {
	view := core.StatusView{
		OK:            true,
		Address:       in.Cfg.Address,
		Platform:      runtime.GOOS,
		AsrProvider:   in.Cfg.ASR.Provider,
		KeymapProfile: in.Cfg.Keymap.Profile,
	}
	keydConfigPath := in.Cfg.KeydConfig
	asrErrValue := in.ASRErr
	for name := range in.Cfg.ASR.Providers {
		view.AsrProviders = append(view.AsrProviders, name)
	}
	for name := range in.Cfg.Keymap.Profiles {
		view.KeymapProfiles = append(view.KeymapProfiles, name)
	}
	sort.Strings(view.AsrProviders)
	sort.Strings(view.KeymapProfiles)

	// The helper's reply stays a map: that is the root daemon's IPC protocol,
	// not a status model this repo owns (R6). These coercions are the only
	// place it is decoded.
	if helperStatus, err := in.Helper.Call(ctx, map[string]string{"cmd": "status"}); err == nil {
		view.KeydActive, _ = helperStatus["keyd_active"].(bool)
		view.RemotePresent, _ = helperStatus["remote_present"].(bool)
		view.Mapping = helper.StringMap(helperStatus["mapping"])
		view.Config = helper.StringValue(helperStatus["config"])
		view.KeyTotal = helper.Int64Value(helperStatus["key_total"])
		view.KeyCounts = helper.Int64Map(helperStatus["key_counts"])
	} else {
		view.OK = false
		view.HelperError = err.Error()
	}
	if view.Config == "" {
		if data, err := os.ReadFile(keydConfigPath); err == nil {
			view.Config = string(data)
		}
	}
	agentStatus := in.Agent.Status()
	view.MicActive = agentStatus.Connected && agentStatus.Ready
	view.MicHealthy = agentStatus.MicHealthy
	view.BleConnected = agentStatus.Connected
	view.AtvvReady = agentStatus.Ready
	view.LastLatencyMS = agentStatus.LastLatency.Milliseconds()
	view.AsrBackend = in.Agent.ASRName()
	view.AsrLocal = agentStatus.ASRLocal
	if asrErrValue != nil {
		view.AsrError = asrErrValue.Error()
	}
	view.LastText = agentStatus.LastText
	if !agentStatus.LastAt.IsZero() {
		view.LastAt = agentStatus.LastAt.Format(time.RFC3339)
	}
	home, _ := os.UserHomeDir()
	_, agentErr := os.Stat(filepath.Join(home, ".config", "systemd", "user", "default.target.wants", "xiaomi-remote-go.service"))
	_, remapErr := os.Stat("/etc/systemd/system/multi-user.target.wants/xiaomi-remote-remap.service")
	view.AgentAutostart = agentErr == nil
	view.RemapAutostart = remapErr == nil
	return view
}

func BuildReady(ctx context.Context, in service.ViewInputs) core.ReadyView {
	view := core.ReadyView{OK: true}
	// The helper's reply stays a map: that is the root daemon's IPC protocol,
	// not a status model this repo owns (R6).
	helperStatus, err := in.Helper.Call(ctx, map[string]string{"cmd": "status"})
	if err != nil {
		view.HelperError = err.Error()
	} else {
		view.KeydActive, _ = helperStatus["keyd_active"].(bool)
		view.RemotePresent, _ = helperStatus["remote_present"].(bool)
	}
	agentStatus := in.Agent.Status()
	view.BleConnected = agentStatus.Connected
	view.AtvvReady = agentStatus.Ready
	view.MicHealthy = agentStatus.MicHealthy
	asrReadyErr := in.ASRErr
	view.AsrReady = asrReadyErr == nil
	if asrReadyErr != nil {
		view.AsrError = asrReadyErr.Error()
	}
	// OPS-06: readiness answers "can dictation work". The ASR chain is the
	// core; the root remapper, the BLE remote and the virtual mic are
	// optional gaps a phone-only setup never fills, so they move to
	// optional_ok instead of failing ok. Until the hardware run confirms
	// the yardstick (roadmap 未验证 #5), the split is:
	// core = agent + ASR + paste chain, optional = helper + BLE.
	// The classification itself is core.Dependency.Core, which the offline
	// doctor also uses so its exit code cannot disagree with ok (OPS-04).
	view.CoreOK = view.AsrReady
	view.OptionalOK = view.KeydActive && view.RemotePresent && view.BleConnected && view.AtvvReady && view.MicHealthy
	view.OK = view.CoreOK
	return view
}

func BuildDevices(ctx context.Context, in service.ViewInputs) core.DevicesView {
	devices := append([]config.DeviceConfig(nil), in.Cfg.Devices...)
	active := in.Cfg.ActiveDevice
	agentConnected := in.Agent.Status().Connected
	bluezStatus := map[string]bluez.DeviceStatus{}
	if known, err := bluez.KnownDevices(ctx); err == nil {
		for _, item := range known {
			bluezStatus[strings.ToUpper(item.Address)] = item
		}
	}
	view := core.DevicesView{OK: true, Active: active}
	view.Devices = make([]core.DeviceView, 0, len(devices))
	for _, item := range devices {
		profile, _ := device.Builtin().Get(item.Profile)
		link := bluezStatus[strings.ToUpper(item.Address)]
		view.Devices = append(view.Devices, core.DeviceView{
			ID:           item.ID,
			Name:         item.Name,
			Address:      item.Address,
			Profile:      item.Profile,
			Enabled:      item.Enabled,
			Active:       item.ID == active,
			Connected:    link.Connected || (item.ID == active && agentConnected),
			Paired:       link.Paired,
			Battery:      int32(link.Battery),
			Capabilities: profile.Capabilities,
			Vendor:       item.Vendor,
			Product:      item.Product,
			NameMatch:    item.NameMatch,
		})
	}
	for _, profile := range device.Builtin().List() {
		view.Profiles = append(view.Profiles, core.ProfileView{
			ID:           profile.ID,
			Name:         profile.Name,
			Transport:    profile.Transport,
			Capabilities: profile.Capabilities,
		})
	}
	return view
}

// Diagnostics builds the redacted troubleshooting summary to attach to an
// issue: no device addresses, no home paths, no speech, no provider bodies.
func BuildDiagnostics(ctx context.Context, in service.ViewInputs, version string) map[string]any {
	status := BuildStatus(ctx, in)
	status.Address = redact.MAC(status.Address)
	// The report promises no speech, and the status carries the last dictated
	// sentence. Dropping it here is what keeps that promise true for a summary
	// the user attaches to a public issue (I7).
	status.LastText = ""
	asrChecks := asr.Diagnose(in.Cfg.ASR)
	checks := make([]any, 0, len(asrChecks))
	for _, item := range asrChecks {
		checks = append(checks, map[string]any{
			"provider": item.Provider,
			"ok":       item.OK,
			"detail":   item.Detail,
		})
	}
	ready := BuildReady(ctx, in)
	// scrubHome walks maps and slices; a typed view is neither, so every string
	// that can carry a path is scrubbed here or a helper socket path under the
	// home directory would reach a shared report (I7).
	ready.HelperError = scrubHomeString(ready.HelperError)
	ready.AsrError = scrubHomeString(ready.AsrError)
	status.Config = scrubHomeString(status.Config)
	status.HelperError = scrubHomeString(status.HelperError)
	status.AsrError = scrubHomeString(status.AsrError)
	return scrubHome(map[string]any{
		"ok":           true,
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"version":      version,
		"platform":     runtime.GOOS + "/" + runtime.GOARCH,
		"ready":        ready,
		"status":       status,
		"asr":          checks,
	}).(map[string]any)
}

// scrubHomeString replaces the home directory prefix with "~" so a shared
// summary carries no personal paths.
func scrubHomeString(value string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return value
	}
	return strings.ReplaceAll(value, home, "~")
}

// scrubHome is scrubHomeString over the free-form parts of the report.
func scrubHome(value any) any {
	switch typed := value.(type) {
	case string:
		return scrubHomeString(typed)
	case map[string]any:
		for key, item := range typed {
			typed[key] = scrubHome(item)
		}
		return typed
	case []any:
		for index, item := range typed {
			typed[index] = scrubHome(item)
		}
		return typed
	default:
		return value
	}
}
