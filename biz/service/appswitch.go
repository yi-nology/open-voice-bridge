package service

import (
	"context"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/remap"
)

// applyAppRule picks the keymap profile for a focused window class and pushes
// its mapping to the root helper at runtime (SayAll 切得准). The configured
// profile is never rewritten: this is a runtime overlay, and an unmatched
// class falls back to the configured profile.
func (s *Service) applyAppRule(ctx context.Context, class string) {
	s.cfgMu.RLock()
	target := s.cfg.Keymap.Profile
	for _, rule := range s.cfg.Keymap.AppRules {
		if remap.MatchClass(class, rule.Class) {
			target = rule.Profile
			break
		}
	}
	mapping, ok := s.cfg.Keymap.Profiles[target]
	s.cfgMu.RUnlock()
	if !ok {
		return
	}
	if _, err := s.helper.Call(ctx, map[string]any{"cmd": "apply", "mapping": remap.Copy(mapping)}); err != nil {
		s.logger.Warn("apply app-rule keymap", "class", class, "profile", target, "err", err)
	}
}

// StartAppSwitcher polls the focused window class and applies the matching
// keymap profile. activeClass is injected so tests do not need an X server.
func (s *Service) StartAppSwitcher(ctx context.Context, poll time.Duration, activeClass func() (string, error)) {
	if poll <= 0 {
		poll = 800 * time.Millisecond
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	last := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			class, err := activeClass()
			if err != nil || class == "" || class == last {
				continue
			}
			last = class
			s.applyAppRule(ctx, class)
		}
	}
}

// AppRules exposes the configured rules for the admin UI.
func (s *Service) AppRules() []config.AppRule {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return append([]config.AppRule(nil), s.cfg.Keymap.AppRules...)
}
