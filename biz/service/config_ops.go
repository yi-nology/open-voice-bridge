// Code split from service.go (ARC-09): one boundary per file.
package service

import (
	"context"
	"fmt"
	oeerrors "github.com/oekit/pkg/errors"
	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/core"
	"github.com/yi-nology/open-voice-bridge/internal/fsutil"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

func (s *Service) SwitchASR(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	s.cfgMu.Lock()
	if _, ok := s.cfg.ASR.Providers[name]; !ok {
		s.cfgMu.Unlock()
		return fmt.Errorf("unknown ASR provider %q", name)
	}
	snapshot := s.configSnapshotLocked()
	snapshot.ASR.Provider = name
	engine, err := s.ops.BuildASR(snapshot.ASR)
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

func (s *Service) ASRConfig(ctx context.Context) config.ASRConfigView {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	// The provider table is owned state (R6): hand out a copy, never the live
	// map — a caller writing into the returned table would otherwise rewrite
	// the running configuration, and readers iterating it outside cfgMu race
	// the next save's writers (#11).
	providers := make(map[string]config.ASRProviderConfig, len(s.cfg.ASR.Providers))
	for name, provider := range s.cfg.ASR.Providers {
		provider.Args = slices.Clone(provider.Args)
		providers[name] = provider
	}
	return config.ASRConfigView{
		OK:        true,
		Provider:  s.cfg.ASR.Provider,
		Fallback:  slices.Clone(s.cfg.ASR.Fallback),
		Language:  s.cfg.ASR.Language,
		Providers: providers,
	}
}

func (s *Service) UpdateASRConfig(ctx context.Context, next config.ASRConfig) error {
	if err := s.ops.ValidateASR(next); err != nil {
		return err
	}
	engine, err := s.ops.BuildASR(next)
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

func (s *Service) TestASR(ctx context.Context, provider string) []core.ASRDiagnostic {
	s.cfgMu.RLock()
	testCfg := s.cfg.ASR
	s.cfgMu.RUnlock()
	testCfg.Provider = provider
	testCfg.Fallback = nil
	if err := s.ops.ValidateASR(testCfg); err != nil {
		return []core.ASRDiagnostic{{Provider: provider, OK: false, Detail: err.Error()}}
	}
	return s.ops.DiagnoseASR(testCfg)
}

func (s *Service) TextConfig(ctx context.Context) config.TextConfigView {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return config.TextConfigView{OK: true, Config: s.cfg.Text}
}

func (s *Service) UpdateTextConfig(ctx context.Context, next config.TextConfig) error {
	rules, err := s.ops.BuildRules(next)
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

// SetLookback saves the 回眸 settings and applies them live. Disabling
// forgets the ring immediately (docs/design/lookback.md).
func (s *Service) SetLookback(enabled bool, seconds int) error {
	if seconds < 5 {
		seconds = 5
	}
	if seconds > 60 {
		seconds = 60
	}
	s.cfgMu.Lock()
	snapshot := s.configSnapshotLocked()
	snapshot.LookbackEnabled = enabled
	snapshot.LookbackSeconds = seconds
	saveErr := s.persistConfig(snapshot)
	if saveErr == nil {
		s.cfg.LookbackEnabled = enabled
		s.cfg.LookbackSeconds = seconds
	}
	s.cfgMu.Unlock()
	if saveErr != nil {
		return saveErr
	}
	s.agent.SetLookback(enabled, seconds)
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
			return fmt.Errorf("unknown rewrite mode %q", rule.Mode)
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
	rules, buildErr := s.ops.BuildRules(s.cfg.Text)
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

func (s *Service) LLMConfig(ctx context.Context) config.LLMConfigView {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return config.LLMConfigView{
		OK:      true,
		Presets: config.LLMPresets(),
		Config:  s.cfg.LLM,
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
		path = filepath.Join(home, ".config", "xiaoyao", "llm.key")
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
	processor, err := s.ops.BuildLLM(next)
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

func (s *Service) TestLLM(ctx context.Context, provider string, live bool) []core.LLMDiagnostic {
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
	diagnostics := s.ops.DiagnoseLLM(testCfg)
	if !live {
		return diagnostics
	}
	processor, err := s.ops.BuildLLM(testCfg)
	if err != nil {
		return append(diagnostics, core.LLMDiagnostic{OK: false, Detail: "build: " + err.Error()})
	}
	latency, pingErr := processor.Ping(ctx)
	if closeErr := processor.Close(ctx); closeErr != nil {
		s.logger.Warn("close tested LLM", "err", closeErr)
	}
	if pingErr != nil {
		return append(diagnostics, core.LLMDiagnostic{OK: false, Detail: "live: " + pingErr.Error()})
	}
	return append(diagnostics, core.LLMDiagnostic{OK: true, Detail: fmt.Sprintf("live: %s", latency.Round(time.Millisecond))})
}
