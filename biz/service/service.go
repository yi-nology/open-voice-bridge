// Package service contains the application operations behind the HTTP API.
package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
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
	InsertPhrase(slot int) (string, bool)
	SetSeqActions(map[int]remap.AgentAction)
	SetRewriteRules([]config.AppRewriteRule)
	SetStreamingSubtitles(bool)
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
	saveMu    sync.Mutex // serialises config disk writes; never held with cfgMu (R8/I3)
	asrErr    error
	agent     Agent
	helper    Helper
	logger    logx.Logger
	mcpTokens *mcp.Store
}

// configSnapshotLocked returns a deep copy of the live configuration for the
// disk write. Callers hold cfgMu only for this copy — the write happens in
// persistConfig, outside the lock (R8/I3).
func (s *Service) configSnapshotLocked() config.Config {
	return s.cfg.Clone()
}

// persistConfig writes a configuration snapshot to disk. It must be called
// without cfgMu held: the write is serialised by saveMu so concurrent commits
// cannot interleave, and a slow disk cannot freeze the config readers (R8, I3,
// CON-08 — the discipline the phone store already applies to its token file).
// The snapshot is a deep copy, so the normalisers inside Save can adjust it
// without ever touching the running state.
func (s *Service) persistConfig(snapshot config.Config) error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	return appconfig.Save(s.path, snapshot)
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
	merged := remap.MergeDefaults(mapping)
	s.cfgMu.Lock()
	if s.cfg.Keymap.Profiles == nil {
		s.cfg.Keymap.Profiles = map[string]map[string]string{}
	}
	snapshot := s.configSnapshotLocked()
	snapshot.Keymap.Profiles["custom"] = merged
	snapshot.Keymap.Profile = "custom"
	s.cfgMu.Unlock()
	if err := s.persistConfig(snapshot); err != nil {
		return nil, err
	}
	s.cfgMu.Lock()
	if s.cfg.Keymap.Profiles == nil {
		s.cfg.Keymap.Profiles = map[string]map[string]string{}
	}
	s.cfg.Keymap.Profiles["custom"] = merged
	s.cfg.Keymap.Profile = "custom"
	s.cfgMu.Unlock()
	// DOM-11: the helper only hears about a mapping that is already on disk;
	// a failed save must not leave the daemon running a config that exists
	// nowhere.
	return s.applyMapping(ctx, mapping)
}

// applyMapping hands a mapping to the remapper after swapping every
// type()/open() seq step for an agent(n) key the user-level agent watches —
// the root daemon only ever presses keys (S3).
func (s *Service) applyMapping(ctx context.Context, mapping map[string]string) (map[string]any, error) {
	rewritten, actions := remap.RewriteAgentActions(mapping)
	s.agent.SetSeqActions(actions)
	return s.helper.Call(ctx, map[string]any{"cmd": "apply", "mapping": rewritten})
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
	snapshot := s.configSnapshotLocked()
	snapshot.ASR.Provider = name
	engine, err := asr.Build(snapshot.ASR)
	if err != nil {
		s.cfgMu.Unlock()
		return err
	}
	s.cfgMu.Unlock()
	if err := s.persistConfig(snapshot); err != nil {
		if closeErr := engine.Close(ctx); closeErr != nil {
			s.logger.Warn("close rejected ASR engine", "err", closeErr)
		}
		return err
	}
	// The saved snapshot is the normalised section; adopting it keeps the
	// running state identical to what the disk now holds.
	s.cfgMu.Lock()
	s.cfg.ASR = snapshot.ASR
	s.asrErr = nil
	s.cfgMu.Unlock()
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
	snapshot := s.configSnapshotLocked()
	snapshot.Keymap.Profile = name
	s.cfgMu.Unlock()
	if err := s.persistConfig(snapshot); err != nil {
		return nil, err
	}
	s.cfgMu.Lock()
	s.cfg.Keymap.Profile = name
	s.cfgMu.Unlock()
	return s.applyMapping(ctx, mapping)
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
	snapshot := s.configSnapshotLocked()
	if snapshot.Keymap.Profiles == nil {
		snapshot.Keymap.Profiles = map[string]map[string]string{}
	}
	snapshot.Keymap.Profiles[name] = cleaned
	snapshot.Keymap.Profile = name
	s.cfgMu.Unlock()
	if err := s.persistConfig(snapshot); err != nil {
		return nil, err
	}
	s.cfgMu.Lock()
	if s.cfg.Keymap.Profiles == nil {
		s.cfg.Keymap.Profiles = map[string]map[string]string{}
	}
	s.cfg.Keymap.Profiles[name] = cleaned
	s.cfg.Keymap.Profile = name
	s.cfgMu.Unlock()
	return s.applyMapping(ctx, cleaned)
}

func (s *Service) DeleteKeymapProfile(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if builtinProfile(name) {
		return oeerrors.Errorf("%q is a built-in profile and cannot be deleted", name)
	}
	s.cfgMu.Lock()
	if s.cfg.Keymap.Profile == name {
		s.cfgMu.Unlock()
		return oeerrors.Errorf("switch to another profile before deleting %q", name)
	}
	if _, ok := s.cfg.Keymap.Profiles[name]; !ok {
		s.cfgMu.Unlock()
		return oeerrors.Errorf("unknown keymap profile %q", name)
	}
	snapshot := s.configSnapshotLocked()
	delete(snapshot.Keymap.Profiles, name)
	s.cfgMu.Unlock()
	if err := s.persistConfig(snapshot); err != nil {
		return err
	}
	s.cfgMu.Lock()
	delete(s.cfg.Keymap.Profiles, name)
	s.cfgMu.Unlock()
	return nil
}

func (s *Service) ASRConfig(ctx context.Context) map[string]any {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	// The provider table is owned state (R6): hand out a copy, never the live
	// map — a caller writing into the returned map would otherwise rewrite the
	// running configuration, and readers iterating it outside cfgMu race the
	// next save's writers (#11).
	providers := make(map[string]config.ASRProviderConfig, len(s.cfg.ASR.Providers))
	for name, provider := range s.cfg.ASR.Providers {
		provider.Args = slices.Clone(provider.Args)
		providers[name] = provider
	}
	fallback := slices.Clone(s.cfg.ASR.Fallback)
	return map[string]any{
		"ok":        true,
		"provider":  s.cfg.ASR.Provider,
		"fallback":  fallback,
		"language":  s.cfg.ASR.Language,
		"providers": providers,
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
	snapshot := s.configSnapshotLocked()
	snapshot.ASR = next
	s.cfgMu.Unlock()
	if err := s.persistConfig(snapshot); err != nil {
		if closeErr := engine.Close(ctx); closeErr != nil {
			s.logger.Warn("close rejected ASR engine", "err", closeErr)
		}
		return err
	}
	// The saved snapshot is the normalised section; adopting it keeps the
	// running state identical to what the disk now holds. A failed save leaves
	// the live section untouched — no rollback window needed.
	s.cfgMu.Lock()
	s.cfg.ASR = snapshot.ASR
	s.asrErr = nil
	s.cfgMu.Unlock()
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
	snapshot := s.configSnapshotLocked()
	snapshot.ActiveDevice = id
	snapshot.Address = item.Address
	s.cfgMu.Unlock()
	if err := s.persistConfig(snapshot); err != nil {
		return err
	}
	s.cfgMu.Lock()
	s.cfg.ActiveDevice = id
	s.cfg.Address = item.Address
	s.cfgMu.Unlock()
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
	snapshot := s.configSnapshotLocked()
	snapshot.Devices[index].Enabled = enabled
	s.cfgMu.Unlock()
	if err := s.persistConfig(snapshot); err != nil {
		return err
	}
	s.cfgMu.Lock()
	s.cfg.Devices[index].Enabled = enabled
	s.cfgMu.Unlock()
	s.syncDevicesEventually()
	return nil
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
	snapshot := s.configSnapshotLocked()
	found := false
	for i := range snapshot.Devices {
		if snapshot.Devices[i].ID == item.ID {
			snapshot.Devices[i] = item
			found = true
			break
		}
	}
	if !found {
		snapshot.Devices = append(snapshot.Devices, item)
	}
	s.cfgMu.Unlock()
	if err := s.persistConfig(snapshot); err != nil {
		return err
	}
	s.cfgMu.Lock()
	found = false
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
	s.cfgMu.Unlock()
	s.syncDevicesEventually()
	return nil
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
			found = true
			break
		}
	}
	if !found {
		s.cfgMu.Unlock()
		return oeerrors.Errorf("unknown device %q", id)
	}
	snapshot := s.configSnapshotLocked()
	for i := range snapshot.Devices {
		if snapshot.Devices[i].ID == id {
			snapshot.Devices = append(snapshot.Devices[:i], snapshot.Devices[i+1:]...)
			break
		}
	}
	s.cfgMu.Unlock()
	if err := s.persistConfig(snapshot); err != nil {
		return err
	}
	s.cfgMu.Lock()
	for i := range s.cfg.Devices {
		if s.cfg.Devices[i].ID == id {
			s.cfg.Devices = append(s.cfg.Devices[:i], s.cfg.Devices[i+1:]...)
			break
		}
	}
	s.cfgMu.Unlock()
	s.syncDevicesEventually()
	return nil
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
	snapshot := s.configSnapshotLocked()
	snapshot.Text = next
	s.cfgMu.Unlock()
	if err := s.persistConfig(snapshot); err != nil {
		return err
	}
	s.cfgMu.Lock()
	s.cfg.Text = next
	s.cfgMu.Unlock()
	s.agent.SetTextRules(rules)
	return nil
}

// StreamingSubtitles reports whether live partial recognition is on (S6).
func (s *Service) StreamingSubtitles() bool {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg.StreamingSubtitles
}

// SetStreamingSubtitles saves the streaming toggle and applies it live.
func (s *Service) SetStreamingSubtitles(on bool) error {
	s.cfgMu.Lock()
	snapshot := s.configSnapshotLocked()
	snapshot.StreamingSubtitles = on
	saveErr := s.persistConfig(snapshot)
	if saveErr == nil {
		s.cfg.StreamingSubtitles = on
	}
	s.cfgMu.Unlock()
	if saveErr != nil {
		return saveErr
	}
	s.agent.SetStreamingSubtitles(on)
	return nil
}

// LLMAppRules returns the app-aware rewrite rules (S5).
func (s *Service) LLMAppRules() []config.AppRewriteRule {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return append([]config.AppRewriteRule(nil), s.cfg.LLM.AppRules...)
}

// UpdateLLMAppRules saves the app-aware rewrite rules and pushes them to the
// live pipeline so the next utterance sees them.
func (s *Service) UpdateLLMAppRules(rules []config.AppRewriteRule) error {
	for _, rule := range rules {
		if strings.TrimSpace(rule.Class) == "" {
			return oeerrors.New("app rewrite rule needs a class name")
		}
		switch rule.Mode {
		case "", "off", "cleanup", "format", "translate":
		default:
			return oeerrors.Errorf("unknown rewrite mode %q", rule.Mode)
		}
	}
	s.cfgMu.Lock()
	snapshot := s.configSnapshotLocked()
	snapshot.LLM.AppRules = rules
	saveErr := s.persistConfig(snapshot)
	if saveErr == nil {
		s.cfg.LLM.AppRules = rules
	}
	s.cfgMu.Unlock()
	if saveErr != nil {
		return saveErr
	}
	s.agent.SetRewriteRules(rules)
	return nil
}

// PastePhrase inserts the phrase bound to slot n into the focused window —
// the picker path of the common-phrase feature (S2).
func (s *Service) PastePhrase(slot int) (string, bool) {
	return s.agent.InsertPhrase(slot)
}

// TextCommands returns the voice-command toggle and map (语音命令). They ride
// a free-form endpoint because the typed text-config model is generated.
func (s *Service) TextCommands() (bool, map[string]string) {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	out := make(map[string]string, len(s.cfg.Text.Commands))
	for spoken, combo := range s.cfg.Text.Commands {
		out[spoken] = combo
	}
	return s.cfg.Text.CommandsEnabled, out
}

// UpdateTextCommands saves the voice-command toggle and map and rebuilds the
// live rules so the change applies to the next utterance.
func (s *Service) UpdateTextCommands(enabled bool, commands map[string]string) error {
	s.cfgMu.Lock()
	previous := s.cfg.Text
	s.cfg.Text.CommandsEnabled = enabled
	s.cfg.Text.Commands = commands
	rules, buildErr := text.New(s.cfg.Text)
	if buildErr == nil {
		buildErr = config.Save(s.path, s.cfg)
	}
	if buildErr != nil {
		s.cfg.Text = previous
	}
	s.cfgMu.Unlock()
	if buildErr != nil {
		return buildErr
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
	needSave := path == ""
	var snapshot config.Config
	if needSave {
		home, err := os.UserHomeDir()
		if err != nil {
			s.cfgMu.Unlock()
			return err
		}
		path = filepath.Join(home, ".config", "xiaomi-remote", "llm.key")
		snapshot = s.configSnapshotLocked()
		snapshot.LLM.APIKeyFile = path
	}
	s.cfgMu.Unlock()
	if needSave {
		if err := s.persistConfig(snapshot); err != nil {
			return err
		}
		s.cfgMu.Lock()
		s.cfg.LLM.APIKeyFile = path
		s.cfgMu.Unlock()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return fsutil.WriteAtomic(path, []byte(key+"\n"), 0o600)
}

func (s *Service) UpdateLLMConfig(ctx context.Context, next config.LLMConfig) error {
	// The generated request model predates app_rules; keep them across saves.
	next.AppRules = s.LLMAppRules()
	processor, err := llm.Build(next)
	if err != nil {
		return err
	}
	s.cfgMu.Lock()
	snapshot := s.configSnapshotLocked()
	snapshot.LLM = next
	s.cfgMu.Unlock()
	if err := s.persistConfig(snapshot); err != nil {
		if processor != nil {
			if closeErr := processor.Close(ctx); closeErr != nil {
				s.logger.Warn("close rejected LLM processor", "err", closeErr)
			}
		}
		return err
	}
	s.cfgMu.Lock()
	s.cfg.LLM = snapshot.LLM
	s.cfgMu.Unlock()
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
		if _, err := s.applyMapping(ctx, mapping); err == nil {
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
