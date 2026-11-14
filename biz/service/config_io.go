package service

import (
	"encoding/json"
	"os"
	"time"

	oeerrors "github.com/oekit/pkg/errors"

	"github.com/yi-nology/open-voice-bridge/internal/appconfig"
	"github.com/yi-nology/open-voice-bridge/internal/config"
)

// ExportConfig returns the live configuration as indented JSON. Provider
// secrets are file paths only; no key material is ever included.
func (s *Service) ExportConfig() ([]byte, error) {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return json.MarshalIndent(s.cfg, "", "  ")
}

// ImportConfig validates an exported config and swaps it in. The previous
// file is kept as a timestamped backup and a rejected document changes
// nothing on disk; the running state picks it up on restart.
// Every disk step (backup read, atomic write, reload) runs outside cfgMu
// (R8/I3): the lock is held only for the final in-memory swap, while saveMu
// serialises the writers so two imports cannot interleave their steps.
func (s *Service) ImportConfig(data []byte) error {
	var incoming config.Config
	if err := json.Unmarshal(data, &incoming); err != nil {
		return oeerrors.Wrap(err, "parse config")
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	if err := s.backupConfig(); err != nil {
		return err
	}
	// Save normalises and validates before writing: a rejected document
	// leaves the previous file exactly as it was.
	if err := appconfig.Save(s.path, incoming); err != nil {
		return err
	}
	fresh, err := appconfig.Load(s.path)
	if err != nil {
		return err
	}
	s.cfgMu.Lock()
	s.cfg = fresh
	s.cfgMu.Unlock()
	return nil
}

// backupConfig copies the current config file aside. It runs without cfgMu:
// the caller holds saveMu, and the read is plain disk IO (R8).
func (s *Service) backupConfig() error {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return oeerrors.Wrap(err, "read current config")
	}
	backup := s.path + ".bak-" + time.Now().Format("20060102-150405")
	return os.WriteFile(backup, data, 0o600)
}
