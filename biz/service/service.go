// Package service contains the application operations behind the HTTP API.
package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/oekit/concurrency/async"
	"github.com/oekit/logx"
	oeerrors "github.com/oekit/pkg/errors"

	"github.com/yi-nology/open-voice-bridge/internal/appconfig"
	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/bluez"
	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/core"
	"github.com/yi-nology/open-voice-bridge/internal/device"
	"github.com/yi-nology/open-voice-bridge/internal/fsutil"
	"github.com/yi-nology/open-voice-bridge/internal/llm"
	"github.com/yi-nology/open-voice-bridge/internal/mcp"
	"github.com/yi-nology/open-voice-bridge/internal/remap"
	"github.com/yi-nology/open-voice-bridge/internal/text"
)

var profileNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

func builtinProfile(name string) bool {
	_, ok := config.DefaultProfiles()[name]
	return ok
}

// Agent is the subset of the BLE/ASR agent used by the service.
type Agent interface {
	Status() core.AgentView
	Reconnect() error
	SetAddress(string) error
	RestartAudio(context.Context) error
	SetEngine(asr.Engine)
	SetLLM(llm.Processor)
	SetTextRules(*text.Rules)
	ASRName() string
}

// Helper is the root remapper client.
type Helper interface {
	Call(context.Context, any) (map[string]any, error)
}

// Service owns runtime configuration and coordinates the agent and remapper.
type Service struct {
	cfg       config.Config
	path      string
	cfgMu     sync.RWMutex
	asrErr    error
	agent     Agent
	helper    Helper
	logger    logx.Logger
	mcpTokens *mcp.Store
}

func New(cfg config.Config, path string, agent Agent, helper Helper, asrErr error, logger logx.Logger) *Service {
	return &Service{
		cfg:       cfg,
		path:      path,
		agent:     agent,
		helper:    helper,
		asrErr:    asrErr,
		logger:    logger,
		mcpTokens: mcp.NewStore(filepath.Join(filepath.Dir(path), "mcp-tokens.json")),
	}
}

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

func (s *Service) Apply(ctx context.Context, mapping map[string]string) (map[string]any, error) {
	s.cfgMu.Lock()
	if s.cfg.Keymap.Profiles == nil {
		s.cfg.Keymap.Profiles = map[string]map[string]string{}
	}
	s.cfg.Keymap.Profiles["custom"] = remap.MergeDefaults(mapping)
	s.cfg.Keymap.Profile = "custom"
	saveErr := appconfig.Save(s.path, s.cfg)
	s.cfgMu.Unlock()
	if saveErr != nil {
		return nil, saveErr
	}
	// DOM-11: the helper only hears about a mapping that is already on disk;
	// a failed save must not leave the daemon running a config that exists
	// nowhere.
	return s.helper.Call(ctx, map[string]any{"cmd": "apply", "mapping": mapping})
}

func (s *Service) Reconnect(ctx context.Context) error { return s.agent.Reconnect() }

func (s *Service) RestartMic(ctx context.Context) error { return s.agent.RestartAudio(ctx) }

func (s *Service) SwitchASR(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	s.cfgMu.Lock()
	if _, ok := s.cfg.ASR.Providers[name]; !ok {
		s.cfgMu.Unlock()
		return oeerrors.Errorf("unknown ASR provider %q", name)
	}
	next := s.cfg.ASR
	next.Provider = name
	engine, err := asr.Build(next)
	if err != nil {
		s.cfgMu.Unlock()
		return err
	}
	s.cfg.ASR.Provider = name
	saveErr := appconfig.Save(s.path, s.cfg)
	if saveErr == nil {
		s.asrErr = nil
	}
	s.cfgMu.Unlock()
	if saveErr != nil {
		if closeErr := engine.Close(ctx); closeErr != nil {
			s.logger.Warn("close rejected ASR engine", "err", closeErr)
		}
		return saveErr
	}
	s.agent.SetEngine(engine)
	return nil
}

func (s *Service) KeymapProfiles(ctx context.Context) (string, map[string]map[string]string) {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	profiles := make(map[string]map[string]string, len(s.cfg.Keymap.Profiles))
	for name, mapping := range s.cfg.Keymap.Profiles {
		profiles[name] = remap.Copy(mapping)
	}
	return s.cfg.Keymap.Profile, profiles
}

func (s *Service) ApplyKeymapProfile(ctx context.Context, name string) (map[string]any, error) {
	s.cfgMu.RLock()
	mapping, ok := s.cfg.Keymap.Profiles[name]
	if ok {
		mapping = remap.Copy(mapping)
	}
	s.cfgMu.RUnlock()
	if !ok {
		return nil, oeerrors.Errorf("unknown keymap profile %q", name)
	}
	s.cfgMu.Lock()
	s.cfg.Keymap.Profile = name
	saveErr := appconfig.Save(s.path, s.cfg)
	s.cfgMu.Unlock()
	if saveErr != nil {
		return nil, saveErr
	}
	return s.helper.Call(ctx, map[string]any{"cmd": "apply", "mapping": mapping})
}

func (s *Service) SaveKeymapProfile(ctx context.Context, name string, mapping map[string]string) (map[string]any, error) {
	name = strings.TrimSpace(name)
	if !profileNamePattern.MatchString(name) {
		return nil, oeerrors.Errorf("profile name must be 1-32 chars of a-z, 0-9, _ or -")
	}
	if builtinProfile(name) {
		return nil, oeerrors.Errorf("%q is a built-in profile and cannot be overwritten", name)
	}
	cleaned := remap.MergeDefaults(mapping)
	if err := remap.Validate(cleaned); err != nil {
		return nil, err
	}
	s.cfgMu.Lock()
	if s.cfg.Keymap.Profiles == nil {
		s.cfg.Keymap.Profiles = map[string]map[string]string{}
	}
	s.cfg.Keymap.Profiles[name] = cleaned
	s.cfg.Keymap.Profile = name
	saveErr := appconfig.Save(s.path, s.cfg)
	s.cfgMu.Unlock()
	if saveErr != nil {
		return nil, saveErr
	}
	return s.helper.Call(ctx, map[string]any{"cmd": "apply", "mapping": cleaned})
}

func (s *Service) DeleteKeymapProfile(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if builtinProfile(name) {
		return oeerrors.Errorf("%q is a built-in profile and cannot be deleted", name)
	}
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	if s.cfg.Keymap.Profile == name {
		return oeerrors.Errorf("switch to another profile before deleting %q", name)
	}
	if _, ok := s.cfg.Keymap.Profiles[name]; !ok {
		return oeerrors.Errorf("unknown keymap profile %q", name)
	}
	delete(s.cfg.Keymap.Profiles, name)
	return appconfig.Save(s.path, s.cfg)
}

func (s *Service) ASRConfig(ctx context.Context) map[string]any {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return map[string]any{
		"ok":        true,
		"provider":  s.cfg.ASR.Provider,
		"fallback":  s.cfg.ASR.Fallback,
		"language":  s.cfg.ASR.Language,
		"providers": s.cfg.ASR.Providers,
	}
}

func (s *Service) UpdateASRConfig(ctx context.Context, next config.ASRConfig) error {
	if err := asr.Validate(next); err != nil {
		return err
	}
	engine, err := asr.Build(next)
	if err != nil {
		return err
	}
	s.cfgMu.Lock()
	previous := s.cfg.ASR
	s.cfg.ASR = next
	saveErr := appconfig.Save(s.path, s.cfg)
	if saveErr != nil {
		s.cfg.ASR = previous
	} else {
		s.asrErr = nil
	}
	s.cfgMu.Unlock()
	if saveErr != nil {
		if closeErr := engine.Close(ctx); closeErr != nil {
			s.logger.Warn("close rejected ASR engine", "err", closeErr)
		}
		return saveErr
	}
	s.agent.SetEngine(engine)
	return nil
}

func (s *Service) TestASR(ctx context.Context, provider string) []asr.Diagnostic {
	s.cfgMu.RLock()
	testCfg := s.cfg.ASR
	s.cfgMu.RUnlock()
	testCfg.Provider = provider
	testCfg.Fallback = nil
	if err := asr.Validate(testCfg); err != nil {
		return []asr.Diagnostic{{Provider: provider, OK: false, Detail: err.Error()}}
	}
	return asr.Diagnose(testCfg)
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

func (s *Service) Devices(ctx context.Context) core.DevicesView {
	s.cfgMu.RLock()
	devices := append([]config.DeviceConfig(nil), s.cfg.Devices...)
	active := s.cfg.ActiveDevice
	s.cfgMu.RUnlock()
	agentConnected := s.agent.Status().Connected
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

func (s *Service) DiscoverDevices(ctx context.Context) map[string]any {
	items, err := device.DiscoverInputDevices()
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	s.cfgMu.RLock()
	configured := make(map[string]bool, len(s.cfg.Devices))
	for _, item := range s.cfg.Devices {
		configured[strings.ToUpper(item.Address)] = true
	}
	s.cfgMu.RUnlock()
	known := map[string]bluez.DeviceStatus{}
	if list, err := bluez.KnownDevices(ctx); err == nil {
		for _, item := range list {
			known[strings.ToUpper(item.Address)] = item
		}
	}
	merged := map[string]map[string]any{}
	for _, item := range items {
		address := strings.ToUpper(item.Address)
		link := known[address]
		profileID := item.Profile
		capabilities := item.Capabilities
		if profileID == "" {
			if profile, ok := device.Builtin().SuggestProfile(item.Name); ok {
				profileID = profile.ID
				capabilities = profile.Capabilities
			}
		}
		merged[address] = map[string]any{
			"address":      address,
			"name":         item.Name,
			"vendor":       item.Vendor,
			"product":      item.Product,
			"profile":      profileID,
			"capabilities": capabilities,
			"configured":   configured[address],
			"paired":       link.Paired,
			"connected":    true,
			"battery":      link.Battery,
		}
	}
	for address, link := range known {
		if _, ok := merged[address]; ok {
			continue
		}
		profileID := ""
		var capabilities []string
		if profile, ok := device.Builtin().MatchIDs(link.Vendor, link.Product); ok {
			profileID = profile.ID
			capabilities = profile.Capabilities
		} else if profile, ok := device.Builtin().MatchName(link.Name); ok {
			profileID = profile.ID
			capabilities = profile.Capabilities
		} else if profile, ok := device.Builtin().SuggestProfile(link.Name); ok {
			profileID = profile.ID
			capabilities = profile.Capabilities
		}
		merged[address] = map[string]any{
			"address":      address,
			"name":         link.Name,
			"vendor":       link.Vendor,
			"product":      link.Product,
			"profile":      profileID,
			"capabilities": capabilities,
			"configured":   configured[address],
			"paired":       link.Paired,
			"connected":    link.Connected,
			"battery":      link.Battery,
		}
	}
	out := make([]map[string]any, 0, len(merged))
	for _, item := range merged {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		return stringValue(out[i]["address"]) < stringValue(out[j]["address"])
	})
	return map[string]any{"ok": true, "devices": out}
}

func (s *Service) SelectDevice(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	s.cfgMu.Lock()
	index := -1
	for i := range s.cfg.Devices {
		if s.cfg.Devices[i].ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		s.cfgMu.Unlock()
		return oeerrors.Errorf("unknown device %q", id)
	}
	item := s.cfg.Devices[index]
	if !item.Enabled {
		s.cfgMu.Unlock()
		return oeerrors.Errorf("device %q is disabled", id)
	}
	profile, ok := device.Builtin().Get(item.Profile)
	if !ok || !profile.HasCapability("voice") {
		s.cfgMu.Unlock()
		return oeerrors.Errorf("device %q does not support voice", id)
	}
	s.cfg.ActiveDevice = id
	s.cfg.Address = item.Address
	saveErr := appconfig.Save(s.path, s.cfg)
	s.cfgMu.Unlock()
	if saveErr != nil {
		return saveErr
	}
	return s.agent.SetAddress(item.Address)
}

func (s *Service) SetDeviceEnabled(ctx context.Context, id string, enabled bool) error {
	id = strings.TrimSpace(id)
	s.cfgMu.Lock()
	index := -1
	for i := range s.cfg.Devices {
		if s.cfg.Devices[i].ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		s.cfgMu.Unlock()
		return oeerrors.Errorf("unknown device %q", id)
	}
	if !enabled && s.cfg.ActiveDevice == id {
		s.cfgMu.Unlock()
		return oeerrors.Errorf("select another device before disabling %q", id)
	}
	s.cfg.Devices[index].Enabled = enabled
	saveErr := appconfig.Save(s.path, s.cfg)
	s.cfgMu.Unlock()
	if saveErr == nil {
		s.syncDevicesEventually()
	}
	return saveErr
}

func (s *Service) ConnectDevice(ctx context.Context, id string, connect bool) error {
	id = strings.TrimSpace(id)
	s.cfgMu.RLock()
	var item config.DeviceConfig
	found := false
	for _, candidate := range s.cfg.Devices {
		if candidate.ID == id {
			item = candidate
			found = true
			break
		}
	}
	active := s.cfg.ActiveDevice == id
	s.cfgMu.RUnlock()
	if !found {
		return oeerrors.Errorf("unknown device %q", id)
	}
	if !item.Enabled {
		return oeerrors.Errorf("device %q is disabled", id)
	}
	profile, _ := device.Builtin().Get(item.Profile)
	if connect && active && profile.HasCapability("voice") {
		return s.agent.SetAddress(item.Address)
	}
	if !connect && active {
		return oeerrors.Errorf("select another device before disconnecting %q", id)
	}
	if connect {
		if err := bluez.Connect(ctx, item.Address); err != nil && !strings.Contains(err.Error(), "Already Connected") {
			return err
		}
		return nil
	}
	if err := bluez.Disconnect(ctx, item.Address); err != nil && !strings.Contains(err.Error(), "Not Connected") {
		return err
	}
	return nil
}

func (s *Service) SaveDevice(ctx context.Context, item config.DeviceConfig) error {
	item.ID = strings.TrimSpace(item.ID)
	item.Name = strings.TrimSpace(item.Name)
	item.Address = strings.TrimSpace(item.Address)
	item.Profile = strings.TrimSpace(item.Profile)
	if item.Profile == "" {
		item.Profile = "rc003"
	}
	if _, ok := device.Builtin().Get(item.Profile); !ok {
		return oeerrors.Errorf("unknown device profile %q", item.Profile)
	}
	if item.ID == "" {
		return oeerrors.New("device id is required")
	}
	s.cfgMu.Lock()
	found := false
	for i := range s.cfg.Devices {
		if s.cfg.Devices[i].ID == item.ID {
			s.cfg.Devices[i] = item
			found = true
			break
		}
	}
	if !found {
		s.cfg.Devices = append(s.cfg.Devices, item)
	}
	saveErr := appconfig.Save(s.path, s.cfg)
	s.cfgMu.Unlock()
	if saveErr == nil {
		s.syncDevicesEventually()
	}
	return saveErr
}

func (s *Service) DeleteDevice(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	s.cfgMu.Lock()
	if s.cfg.ActiveDevice == id {
		s.cfgMu.Unlock()
		return oeerrors.Errorf("select another device before deleting %q", id)
	}
	if len(s.cfg.Devices) <= 1 {
		s.cfgMu.Unlock()
		return oeerrors.New("cannot delete the last device")
	}
	found := false
	for i := range s.cfg.Devices {
		if s.cfg.Devices[i].ID == id {
			s.cfg.Devices = append(s.cfg.Devices[:i], s.cfg.Devices[i+1:]...)
			found = true
			break
		}
	}
	if !found {
		s.cfgMu.Unlock()
		return oeerrors.Errorf("unknown device %q", id)
	}
	saveErr := appconfig.Save(s.path, s.cfg)
	s.cfgMu.Unlock()
	if saveErr == nil {
		s.syncDevicesEventually()
	}
	return saveErr
}

func (s *Service) syncDevicesEventually() {
	async.GoSafe(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
		defer cancel()
		s.SyncDevices(ctx)
	})
}

func (s *Service) TextConfig(ctx context.Context) map[string]any {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return map[string]any{"ok": true, "config": s.cfg.Text}
}

func (s *Service) UpdateTextConfig(ctx context.Context, next config.TextConfig) error {
	rules, err := text.New(next)
	if err != nil {
		return err
	}
	s.cfgMu.Lock()
	previous := s.cfg.Text
	s.cfg.Text = next
	saveErr := appconfig.Save(s.path, s.cfg)
	if saveErr != nil {
		s.cfg.Text = previous
	}
	s.cfgMu.Unlock()
	if saveErr != nil {
		return saveErr
	}
	s.agent.SetTextRules(rules)
	return nil
}

func (s *Service) LLMConfig(ctx context.Context) map[string]any {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return map[string]any{
		"ok":      true,
		"presets": config.LLMPresets(),
		"config":  s.cfg.LLM,
	}
}

func (s *Service) SaveLLMKey(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return oeerrors.New("llm API key is empty")
	}
	s.cfgMu.Lock()
	path := strings.TrimSpace(s.cfg.LLM.APIKeyFile)
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			s.cfgMu.Unlock()
			return err
		}
		path = filepath.Join(home, ".config", "xiaomi-remote", "llm.key")
		s.cfg.LLM.APIKeyFile = path
		if err := appconfig.Save(s.path, s.cfg); err != nil {
			s.cfgMu.Unlock()
			return err
		}
	}
	s.cfgMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return fsutil.WriteAtomic(path, []byte(key+"\n"), 0o600)
}

func (s *Service) UpdateLLMConfig(ctx context.Context, next config.LLMConfig) error {
	processor, err := llm.Build(next)
	if err != nil {
		return err
	}
	s.cfgMu.Lock()
	previous := s.cfg.LLM
	s.cfg.LLM = next
	saveErr := appconfig.Save(s.path, s.cfg)
	if saveErr != nil {
		s.cfg.LLM = previous
	}
	s.cfgMu.Unlock()
	if saveErr != nil {
		if processor != nil {
			if closeErr := processor.Close(ctx); closeErr != nil {
				s.logger.Warn("close rejected LLM processor", "err", closeErr)
			}
		}
		return saveErr
	}
	s.agent.SetLLM(processor)
	return nil
}

func (s *Service) TestLLM(ctx context.Context, provider string, live bool) []llm.Diagnostic {
	s.cfgMu.RLock()
	testCfg := s.cfg.LLM
	s.cfgMu.RUnlock()
	testCfg.Enabled = true
	if provider != "" && provider != testCfg.Provider {
		if preset, ok := config.LLMPresetByID(provider); ok {
			testCfg.BaseURL = preset.BaseURL
			testCfg.Model = preset.Model
		}
		testCfg.Provider = provider
	}
	diagnostics := llm.Diagnose(testCfg)
	if !live {
		return diagnostics
	}
	processor, err := llm.Build(testCfg)
	if err != nil {
		return append(diagnostics, llm.Diagnostic{OK: false, Detail: "build: " + err.Error()})
	}
	latency, pingErr := processor.Ping(ctx)
	if closeErr := processor.Close(ctx); closeErr != nil {
		s.logger.Warn("close tested LLM", "err", closeErr)
	}
	if pingErr != nil {
		return append(diagnostics, llm.Diagnostic{OK: false, Detail: "live: " + pingErr.Error()})
	}
	return append(diagnostics, llm.Diagnostic{OK: true, Detail: fmt.Sprintf("live: %s", latency.Round(time.Millisecond))})
}

// SyncDevices sends the configured input matchers and the phone key policy to
// the root remapper. The helper refuses KEY_POWER until this arrives (ARC-12).
func (s *Service) SyncDevices(ctx context.Context) {
	var lastErr error
	for attempt := 0; attempt < 30; attempt++ {
		s.cfgMu.RLock()
		devices := append([]config.DeviceConfig(nil), s.cfg.Devices...)
		allowPower := s.cfg.Phone.AllowPowerKey
		s.cfgMu.RUnlock()
		matches := make([]device.Match, 0, len(devices))
		appleEnabled := false
		for _, item := range devices {
			if !item.Enabled {
				continue
			}
			profile, ok := device.Builtin().Get(item.Profile)
			if !ok {
				continue
			}
			match := profile.Match
			if item.Vendor != "" {
				match.Vendor = item.Vendor
			}
			if item.Product != "" {
				match.Product = item.Product
			}
			if len(item.NameMatch) > 0 {
				match.Names = item.NameMatch
			}
			matches = append(matches, match)
			if profile.Transport == "hidraw-apple" {
				appleEnabled = true
			}
		}
		if _, err := s.helper.Call(ctx, map[string]any{"cmd": "set_devices", "devices": matches, "apple": appleEnabled, "allow_power": allowPower}); err == nil {
			s.logger.Info("device matchers applied", "count", len(matches), "apple", appleEnabled, "allow_power", allowPower)
			return
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
	s.logger.Warn("device matchers not applied on startup", "err", lastErr)
}

// SyncKeymap retries until the configured profile has been applied to the
// root remapper or the context is canceled.
func (s *Service) SyncKeymap(ctx context.Context) {
	var lastErr error
	for attempt := 0; attempt < 30; attempt++ {
		s.cfgMu.RLock()
		profile := s.cfg.Keymap.Profile
		mapping, ok := s.cfg.Keymap.Profiles[profile]
		if ok {
			mapping = remap.Copy(mapping)
		}
		s.cfgMu.RUnlock()
		if !ok {
			s.logger.Warn("keymap profile missing", "profile", profile)
			return
		}
		if _, err := s.helper.Call(ctx, map[string]any{"cmd": "apply", "mapping": mapping}); err == nil {
			s.logger.Info("keymap profile applied", "profile", profile)
			return
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
	s.logger.Warn("keymap profile not applied on startup", "err", lastErr)
}

// PhoneEnabled reports the saved phone-input intent, which can differ from
// the running listener until the service restarts (OPS-01).
func (s *Service) PhoneEnabled() bool {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg.Phone.Enabled
}

// SetPhoneEnabled turns the phone input source on or off in the saved
// configuration (OPS-01). The listener starts with the service, so callers
// must ask for a restart — same contract as a config import. The write goes
// through config.Save, so an invalid phone section is rejected (SEC-03/DOM-08)
// and a failed save never leaves the in-memory flag flipped.
func (s *Service) SetPhoneEnabled(enabled bool) error {
	s.cfgMu.Lock()
	previous := s.cfg.Phone.Enabled
	s.cfg.Phone.Enabled = enabled
	saveErr := config.Save(s.path, s.cfg)
	if saveErr != nil {
		s.cfg.Phone.Enabled = previous
	}
	s.cfgMu.Unlock()
	return saveErr
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func stringMap(value any) map[string]string {
	switch typed := value.(type) {
	case map[string]string:
		return typed
	case map[string]any:
		out := make(map[string]string, len(typed))
		for key, item := range typed {
			if text, ok := item.(string); ok {
				out[key] = text
			}
		}
		return out
	default:
		return nil
	}
}

func int64Map(value any) map[string]int64 {
	switch typed := value.(type) {
	case map[string]int64:
		return typed
	case map[string]any:
		out := make(map[string]int64, len(typed))
		for key, item := range typed {
			out[key] = int64Value(item)
		}
		return out
	default:
		return nil
	}
}

func int64Value(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int32:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	default:
		return 0
	}
}
