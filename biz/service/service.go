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

	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/bluez"
	"github.com/yi-nology/open-voice-bridge/internal/config"
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
	Status() map[string]any
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

func (s *Service) Status(ctx context.Context) map[string]any {
	s.cfgMu.RLock()
	address := s.cfg.Address
	keydConfigPath := s.cfg.KeydConfig
	asrProvider := s.cfg.ASR.Provider
	asrProviders := make([]string, 0, len(s.cfg.ASR.Providers))
	for name := range s.cfg.ASR.Providers {
		asrProviders = append(asrProviders, name)
	}
	keymapProfile := s.cfg.Keymap.Profile
	keymapProfiles := make([]string, 0, len(s.cfg.Keymap.Profiles))
	for name := range s.cfg.Keymap.Profiles {
		keymapProfiles = append(keymapProfiles, name)
	}
	asrErrValue := s.asrErr
	s.cfgMu.RUnlock()
	sort.Strings(asrProviders)
	sort.Strings(keymapProfiles)

	status := map[string]any{"ok": true, "address": address, "platform": runtime.GOOS}
	if helperStatus, err := s.helper.Call(ctx, map[string]string{"cmd": "status"}); err == nil {
		for _, key := range []string{"keyd_active", "remote_present", "mapping", "config", "key_total", "key_counts"} {
			if value, ok := helperStatus[key]; ok {
				status[key] = value
			}
		}
	} else {
		status["ok"] = false
		status["keyd_active"] = false
		status["remote_present"] = false
		status["helper_error"] = err.Error()
	}
	if _, ok := status["config"]; !ok {
		if data, err := os.ReadFile(keydConfigPath); err == nil {
			status["config"] = string(data)
		}
	}
	agentStatus := s.agent.Status()
	connected, _ := agentStatus["connected"].(bool)
	ready, _ := agentStatus["ready"].(bool)
	status["mic_active"] = connected && ready
	status["mic_healthy"] = agentStatus["mic_healthy"]
	status["ble_connected"] = connected
	status["atvv_ready"] = ready
	status["last_latency_ms"] = agentStatus["last_latency_ms"]
	status["asr_backend"] = s.agent.ASRName()
	status["asr_local"] = agentStatus["asr_local"]
	if asrErrValue != nil {
		status["asr_error"] = asrErrValue.Error()
	}
	status["last_text"] = agentStatus["last_text"]
	status["last_at"] = agentStatus["last_at"]
	status["asr_provider"] = asrProvider
	status["asr_providers"] = asrProviders
	status["keymap_profile"] = keymapProfile
	status["keymap_profiles"] = keymapProfiles
	home, _ := os.UserHomeDir()
	_, agentErr := os.Stat(filepath.Join(home, ".config", "systemd", "user", "default.target.wants", "xiaomi-remote-go.service"))
	_, remapErr := os.Stat("/etc/systemd/system/multi-user.target.wants/xiaomi-remote-remap.service")
	status["agent_autostart"] = agentErr == nil
	status["remap_autostart"] = remapErr == nil
	return status
}

func (s *Service) Apply(ctx context.Context, mapping map[string]string) (map[string]any, error) {
	s.cfgMu.Lock()
	if s.cfg.Keymap.Profiles == nil {
		s.cfg.Keymap.Profiles = map[string]map[string]string{}
	}
	s.cfg.Keymap.Profiles["custom"] = remap.MergeDefaults(mapping)
	s.cfg.Keymap.Profile = "custom"
	saveErr := config.Save(s.path, s.cfg)
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
	saveErr := config.Save(s.path, s.cfg)
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
	saveErr := config.Save(s.path, s.cfg)
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
	saveErr := config.Save(s.path, s.cfg)
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
	return config.Save(s.path, s.cfg)
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

func (s *Service) UpdateASRConfig(ctx context.Context, next asr.Config) error {
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
	saveErr := config.Save(s.path, s.cfg)
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

func (s *Service) Ready(ctx context.Context) map[string]any {
	checks := map[string]any{"ok": true}
	helperStatus, err := s.helper.Call(ctx, map[string]string{"cmd": "status"})
	keyd := false
	remote := false
	if err != nil {
		checks["ok"] = false
		checks["helper_error"] = err.Error()
	} else {
		keyd, _ = helperStatus["keyd_active"].(bool)
		remote, _ = helperStatus["remote_present"].(bool)
	}
	agentStatus := s.agent.Status()
	connected, _ := agentStatus["connected"].(bool)
	atvvReady, _ := agentStatus["ready"].(bool)
	micHealthy, micKnown := agentStatus["mic_healthy"].(bool)
	checks["keyd_active"] = keyd
	checks["remote_present"] = remote
	checks["ble_connected"] = connected
	checks["atvv_ready"] = atvvReady
	checks["mic_healthy"] = micKnown && micHealthy
	s.cfgMu.RLock()
	asrReadyErr := s.asrErr
	s.cfgMu.RUnlock()
	checks["asr_ready"] = asrReadyErr == nil
	if asrReadyErr != nil {
		checks["asr_error"] = asrReadyErr.Error()
	}
	if !keyd || !remote || !connected || !atvvReady || !micKnown || !micHealthy || asrReadyErr != nil {
		checks["ok"] = false
	}
	return checks
}

func (s *Service) Devices(ctx context.Context) map[string]any {
	s.cfgMu.RLock()
	devices := append([]config.DeviceConfig(nil), s.cfg.Devices...)
	active := s.cfg.ActiveDevice
	s.cfgMu.RUnlock()
	agentConnected := false
	if status := s.agent.Status(); status != nil {
		agentConnected, _ = status["connected"].(bool)
	}
	bluezStatus := map[string]bluez.DeviceStatus{}
	if known, err := bluez.KnownDevices(ctx); err == nil {
		for _, item := range known {
			bluezStatus[strings.ToUpper(item.Address)] = item
		}
	}
	out := make([]map[string]any, 0, len(devices))
	for _, item := range devices {
		profile, _ := device.Builtin().Get(item.Profile)
		link := bluezStatus[strings.ToUpper(item.Address)]
		connected := link.Connected || (item.ID == active && agentConnected)
		out = append(out, map[string]any{
			"id":           item.ID,
			"name":         item.Name,
			"address":      item.Address,
			"profile":      item.Profile,
			"enabled":      item.Enabled,
			"active":       item.ID == active,
			"connected":    connected,
			"paired":       link.Paired,
			"battery":      link.Battery,
			"capabilities": profile.Capabilities,
			"vendor":       item.Vendor,
			"product":      item.Product,
			"name_match":   item.NameMatch,
		})
	}
	profiles := make([]map[string]any, 0)
	for _, profile := range device.Builtin().List() {
		profiles = append(profiles, map[string]any{
			"id":           profile.ID,
			"name":         profile.Name,
			"transport":    profile.Transport,
			"capabilities": profile.Capabilities,
		})
	}
	return map[string]any{"ok": true, "active": active, "devices": out, "profiles": profiles}
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
	saveErr := config.Save(s.path, s.cfg)
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
	saveErr := config.Save(s.path, s.cfg)
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
	saveErr := config.Save(s.path, s.cfg)
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
	saveErr := config.Save(s.path, s.cfg)
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

func (s *Service) UpdateTextConfig(ctx context.Context, next text.Config) error {
	rules, err := text.New(next)
	if err != nil {
		return err
	}
	s.cfgMu.Lock()
	previous := s.cfg.Text
	s.cfg.Text = next
	saveErr := config.Save(s.path, s.cfg)
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
		"presets": llm.Presets(),
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
		if err := config.Save(s.path, s.cfg); err != nil {
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

func (s *Service) UpdateLLMConfig(ctx context.Context, next llm.Config) error {
	processor, err := llm.Build(next)
	if err != nil {
		return err
	}
	s.cfgMu.Lock()
	previous := s.cfg.LLM
	s.cfg.LLM = next
	saveErr := config.Save(s.path, s.cfg)
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
		if preset, ok := llm.PresetByID(provider); ok {
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

// SyncDevices sends the configured input matchers to the root remapper.
func (s *Service) SyncDevices(ctx context.Context) {
	var lastErr error
	for attempt := 0; attempt < 30; attempt++ {
		s.cfgMu.RLock()
		devices := append([]config.DeviceConfig(nil), s.cfg.Devices...)
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
		if _, err := s.helper.Call(ctx, map[string]any{"cmd": "set_devices", "devices": matches, "apple": appleEnabled}); err == nil {
			s.logger.Info("device matchers applied", "count", len(matches), "apple", appleEnabled)
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

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}
