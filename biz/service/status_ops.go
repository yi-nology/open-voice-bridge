// Code split from service.go (ARC-09): one boundary per file.
package service

import (
	"context"
	"github.com/yi-nology/open-voice-bridge/internal/core"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"
)

func (s *Service) Status(ctx context.Context) core.StatusView {
	s.cfgMu.RLock()
	view := core.StatusView{
		OK:            true,
		Address:       s.cfg.Address,
		Platform:      runtime.GOOS,
		AsrProvider:   s.cfg.ASR.Provider,
		KeymapProfile: s.cfg.Keymap.Profile,
	}
	keydConfigPath := s.cfg.KeydConfig
	asrErrValue := s.asrErr
	for name := range s.cfg.ASR.Providers {
		view.AsrProviders = append(view.AsrProviders, name)
	}
	for name := range s.cfg.Keymap.Profiles {
		view.KeymapProfiles = append(view.KeymapProfiles, name)
	}
	s.cfgMu.RUnlock()
	sort.Strings(view.AsrProviders)
	sort.Strings(view.KeymapProfiles)

	// The helper's reply stays a map: that is the root daemon's IPC protocol,
	// not a status model this repo owns (R6). These coercions are the only
	// place it is decoded.
	if helperStatus, err := s.helper.Call(ctx, map[string]string{"cmd": "status"}); err == nil {
		view.KeydActive, _ = helperStatus["keyd_active"].(bool)
		view.RemotePresent, _ = helperStatus["remote_present"].(bool)
		view.Mapping = stringMap(helperStatus["mapping"])
		view.Config = stringValue(helperStatus["config"])
		view.KeyTotal = int64Value(helperStatus["key_total"])
		view.KeyCounts = int64Map(helperStatus["key_counts"])
	} else {
		view.OK = false
		view.HelperError = err.Error()
	}
	if view.Config == "" {
		if data, err := os.ReadFile(keydConfigPath); err == nil {
			view.Config = string(data)
		}
	}
	agentStatus := s.agent.Status()
	view.MicActive = agentStatus.Connected && agentStatus.Ready
	view.MicHealthy = agentStatus.MicHealthy
	view.BleConnected = agentStatus.Connected
	view.AtvvReady = agentStatus.Ready
	view.LastLatencyMS = agentStatus.LastLatency.Milliseconds()
	view.AsrBackend = s.agent.ASRName()
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

func (s *Service) Ready(ctx context.Context) core.ReadyView {
	view := core.ReadyView{OK: true}
	// The helper's reply stays a map: that is the root daemon's IPC protocol,
	// not a status model this repo owns (R6).
	helperStatus, err := s.helper.Call(ctx, map[string]string{"cmd": "status"})
	if err != nil {
		view.HelperError = err.Error()
	} else {
		view.KeydActive, _ = helperStatus["keyd_active"].(bool)
		view.RemotePresent, _ = helperStatus["remote_present"].(bool)
	}
	agentStatus := s.agent.Status()
	view.BleConnected = agentStatus.Connected
	view.AtvvReady = agentStatus.Ready
	view.MicHealthy = agentStatus.MicHealthy
	s.cfgMu.RLock()
	asrReadyErr := s.asrErr
	s.cfgMu.RUnlock()
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
