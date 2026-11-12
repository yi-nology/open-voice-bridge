package service

import (
	"context"
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"time"

	oeerrors "github.com/oekit/pkg/errors"

	"github.com/yi-nology/open-voice-bridge/internal/appconfig"
	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/redact"
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

// Diagnostics builds the redacted troubleshooting summary to attach to an
// issue: no device addresses, no home paths, no speech, no provider bodies.
func (s *Service) Diagnostics(ctx context.Context, version string) map[string]any {
	status := s.Status(ctx)
	status.Address = redact.MAC(status.Address)
	// The report promises no speech, and the status carries the last dictated
	// sentence. Dropping it here is what keeps that promise true for a summary
	// the user attaches to a public issue (I7).
	status.LastText = ""
	s.cfgMu.RLock()
	asrChecks := asr.Diagnose(s.cfg.ASR)
	s.cfgMu.RUnlock()
	checks := make([]any, 0, len(asrChecks))
	for _, item := range asrChecks {
		checks = append(checks, map[string]any{
			"provider": item.Provider,
			"ok":       item.OK,
			"detail":   item.Detail,
		})
	}
	ready := s.Ready(ctx)
	// scrubHome walks maps and slices; a typed view is neither, so every string
	// that can carry a path is scrubbed here or a helper socket path under the
	// home directory would reach a shared report (I7).
	ready.HelperError = scrubHomeString(ready.HelperError)
	ready.AsrError = scrubHomeString(ready.AsrError)
	status.Config = scrubHomeString(status.Config)
	status.HelperError = scrubHomeString(status.HelperError)
	status.AsrError = scrubHomeString(status.AsrError)
	return scrubHome(map[string]any{
		"ok":           true,
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"version":      version,
		"platform":     runtime.GOOS + "/" + runtime.GOARCH,
		"ready":        ready,
		"status":       status,
		"asr":          checks,
	}).(map[string]any)
}

// scrubHomeString replaces the home directory prefix with "~" so a shared
// summary carries no personal paths.
func scrubHomeString(value string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return value
	}
	return strings.ReplaceAll(value, home, "~")
}

// scrubHome is scrubHomeString over the free-form parts of the report.
func scrubHome(value any) any {
	switch typed := value.(type) {
	case string:
		return scrubHomeString(typed)
	case map[string]any:
		for key, item := range typed {
			typed[key] = scrubHome(item)
		}
		return typed
	case []any:
		for index, item := range typed {
			typed[index] = scrubHome(item)
		}
		return typed
	default:
		return value
	}
}
