// Package service contains the application operations behind the HTTP API.
package service

import (
	"context"
	"regexp"
	"sync"
	"time"

	"github.com/oekit/logx"

	"github.com/yi-nology/open-voice-bridge/internal/appconfig"
	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/core"
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
	SetEngine(core.ASREngine)
	SetLLM(core.LLMProcessor)
	SetTextRules(core.TextRules)
	ASRName() string
	InsertPhrase(slot int) (string, bool)
	SetSeqActions(map[int]core.AgentAction)
	SetRewriteRules([]config.AppRewriteRule)
	SetStreamingSubtitles(bool)
	SetWake(enabled bool, word string)
	WakeStatus() (bool, error)
	SetLookback(enabled bool, seconds int)
	RecoverLookback(seconds int) bool
}

// Helper is the root remapper client.
type Helper interface {
	Call(context.Context, core.HelperRequest) (core.HelperResponse, error)
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
	mcpTokens core.TokenStore
	ops       Ops
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

// HistoryPolicy returns the retention window and address redaction (波 3).
func (s *Service) HistoryPolicy() (int, bool) {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg.HistoryRetentionDays, s.cfg.HistoryRedactAddresses
}

// SetHistoryPolicy saves the history policy. The store applies redaction on
// future rows; retention is enforced on the next load/append.
func (s *Service) SetHistoryPolicy(retentionDays int, redactAddresses bool) error {
	if retentionDays < 0 {
		retentionDays = 0
	}
	s.cfgMu.Lock()
	snapshot := s.configSnapshotLocked()
	snapshot.HistoryRetentionDays = retentionDays
	snapshot.HistoryRedactAddresses = redactAddresses
	saveErr := s.persistConfig(snapshot)
	if saveErr == nil {
		s.cfg.HistoryRetentionDays = retentionDays
		s.cfg.HistoryRedactAddresses = redactAddresses
	}
	s.cfgMu.Unlock()
	return saveErr
}

// ViewInputs snapshots the raw material the typed view builders need
// (ARC-09): the builders live in app/runtime, this service owns state.
type ViewInputs struct {
	Cfg    config.Config
	ASRErr error
	Agent  AgentProbe
	Helper Helper
}

// AgentProbe is the slice of Agent the view builders need — keeping the
// builders in app/runtime must not drag the whole Agent interface along.
type AgentProbe interface {
	Status() core.AgentView
	ASRName() string
}

// ViewInputs returns a consistent snapshot for the view builders.
func (s *Service) ViewInputs() ViewInputs {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return ViewInputs{Cfg: s.cfg, ASRErr: s.asrErr, Agent: s.agent, Helper: s.helper}
}

// Ops lends service the concrete capabilities it must not construct
// (ARC-09): the composition root builds engines and owns the OS calls, so
// this package stays a state owner rather than a second composition root.
// Engine/token vocabulary is core's (core 类型迁移); behaviour that talks to
// a provider, the OS or a file arrives here as a function value.
type Ops struct {
	BuildASR        func(config.ASRConfig) (core.ASREngine, error)
	BuildLLM        func(config.LLMConfig) (core.LLMProcessor, error)
	BuildRules      func(config.TextConfig) (core.TextRules, error)
	ValidateASR     func(config.ASRConfig) error
	DiagnoseASR     func(config.ASRConfig) []core.ASRDiagnostic
	DiagnoseLLM     func(config.LLMConfig) []core.LLMDiagnostic
	Connect         func(context.Context, string) error
	Disconnect      func(context.Context, string) error
	MergeDefaults   func(map[string]string) map[string]string
	ValidateMapping func(map[string]string) error
	RewriteActions  func(map[string]string) (map[string]string, map[int]core.AgentAction)
	Devices         core.DeviceCatalog
	AppendAudit     func(core.AuditEntry) error
	Tokens          core.TokenStore
}

func New(cfg config.Config, path string, agent Agent, helper Helper, asrErr error, logger logx.Logger, ops Ops) *Service {
	return &Service{
		ops:       ops,
		cfg:       cfg,
		path:      path,
		agent:     agent,
		helper:    helper,
		asrErr:    asrErr,
		logger:    logger,
		mcpTokens: ops.Tokens,
	}
}

func (s *Service) Reconnect(ctx context.Context) error { return s.agent.Reconnect() }

func (s *Service) RestartMic(ctx context.Context) error { return s.agent.RestartAudio(ctx) }

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

// SetWake saves the wake-word settings and restarts the listener.
func (s *Service) SetWake(enabled bool, word string) error {
	if word == "" {
		word = "小遥小遥"
	}
	s.cfgMu.Lock()
	snapshot := s.configSnapshotLocked()
	snapshot.WakeEnabled = enabled
	snapshot.WakeWord = word
	saveErr := s.persistConfig(snapshot)
	if saveErr == nil {
		s.cfg.WakeEnabled = enabled
		s.cfg.WakeWord = word
	}
	s.cfgMu.Unlock()
	if saveErr != nil {
		return saveErr
	}
	s.agent.SetWake(enabled, word)
	return nil
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
		matches := make([]core.DeviceMatch, 0, len(devices))
		appleEnabled := false
		for _, item := range devices {
			if !item.Enabled {
				continue
			}
			profile, ok := s.ops.Devices.Get(item.Profile)
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
		if _, err := s.helper.Call(ctx, core.HelperRequest{Cmd: "set_devices", Devices: matches, Apple: appleEnabled, AllowPower: allowPower}); err == nil {
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
			mapping = core.CopyMapping(mapping)
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
