package service

import (
	"strings"

	"github.com/yi-nology/open-voice-bridge/internal/config"
)

// PasteConfig reports the text-injection settings.
func (s *Service) PasteConfig() (bool, string) {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg.PasteEnabled, s.cfg.PasteBackend
}

// SetPasteConfig stores the injection toggle and backend. Validate runs before
// the write so a typo cannot reach the file; the backend is resolved when the
// pipeline is built, so the caller asks for a restart.
func (s *Service) SetPasteConfig(enabled bool, backend string) error {
	backend = strings.ToLower(strings.TrimSpace(backend))
	if backend == "" {
		backend = "auto"
	}
	s.cfgMu.Lock()
	previousEnabled, previousBackend := s.cfg.PasteEnabled, s.cfg.PasteBackend
	s.cfg.PasteEnabled, s.cfg.PasteBackend = enabled, backend
	if err := s.cfg.Validate(); err != nil {
		s.cfg.PasteEnabled, s.cfg.PasteBackend = previousEnabled, previousBackend
		s.cfgMu.Unlock()
		return err
	}
	saveErr := config.Save(s.path, s.cfg)
	if saveErr != nil {
		s.cfg.PasteEnabled, s.cfg.PasteBackend = previousEnabled, previousBackend
	}
	s.cfgMu.Unlock()
	return saveErr
}
