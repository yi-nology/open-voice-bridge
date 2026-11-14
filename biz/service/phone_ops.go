// Code split from config_ops.go (ARC-09): the phone/wake/lookback side.
package service

import (
	"github.com/yi-nology/open-voice-bridge/internal/config"
)

// StreamingSubtitles reports whether live partial recognition is on (S6).
func (s *Service) StreamingSubtitles() bool {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg.StreamingSubtitles
}

// WakeConfig returns the wake-word toggle and word (S7).
func (s *Service) WakeConfig() (bool, string) {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	word := s.cfg.WakeWord
	if word == "" {
		word = "小遥小遥"
	}
	return s.cfg.WakeEnabled, word
}

// PhonePowerKey reports whether phones may press the power key (OPS-15).
func (s *Service) PhonePowerKey() bool {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg.Phone.AllowPowerKey
}

// SetPhonePowerKey saves the dangerous-key opt-in. Like the rest of
// phone.* it takes effect on restart (ARC-12).
func (s *Service) SetPhonePowerKey(allow bool) error {
	s.cfgMu.Lock()
	snapshot := s.configSnapshotLocked()
	snapshot.Phone.AllowPowerKey = allow
	saveErr := s.persistConfig(snapshot)
	if saveErr == nil {
		s.cfg.Phone.AllowPowerKey = allow
	}
	s.cfgMu.Unlock()
	return saveErr
}

// LookbackConfig returns the 回眸 toggle and window (S8).
func (s *Service) LookbackConfig() (bool, int) {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	seconds := s.cfg.LookbackSeconds
	if seconds == 0 {
		seconds = 30
	}
	return s.cfg.LookbackEnabled, seconds
}

// RecoverLookback transcribes the last seconds of the 回眸 ring (S8).
func (s *Service) RecoverLookback(seconds int) bool {
	return s.agent.RecoverLookback(seconds)
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
