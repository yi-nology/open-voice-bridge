// Package service contains the application operations behind the HTTP API.
package service

import (
	"context"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/oekit/logx"

	"github.com/yi-nology/open-voice-bridge/internal/appconfig"
	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/core"
	"github.com/yi-nology/open-voice-bridge/internal/device"
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
	SetWake(enabled bool, word string)
	WakeStatus() (bool, error)
	SetLookback(enabled bool, seconds int)
	RecoverLookback(seconds int) bool
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
