package appconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yi-nology/open-voice-bridge/internal/config"
)

// The subsystem semantics (provider catalogue, keymap vocabulary, device
// profile table) must still gate every write once config itself stops knowing
// about them (ARC-03, R1).
func TestSaveRejectsUnknownDeviceProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Default()
	cfg.Devices[0].Profile = "no-such-profile"
	if err := Save(path, cfg); err == nil {
		t.Fatal("an unknown device profile must be rejected")
	}
	assertNotWritten(t, path)
}

func TestSaveRejectsUnknownKeymapAction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Default()
	cfg.Keymap.Profiles["coding"]["f5"] = "not-an-action"
	if err := Save(path, cfg); err == nil {
		t.Fatal("an unknown keymap action must be rejected")
	}
	assertNotWritten(t, path)
}

func TestSaveRejectsUndefinedASRProvider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Default()
	cfg.ASR.Provider = "ghost"
	if err := Save(path, cfg); err == nil {
		t.Fatal("a referenced but undefined ASR provider must be rejected")
	}
	assertNotWritten(t, path)
}

func TestSaveAcceptsTheDefaultConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(path, config.Default()); err != nil {
		t.Fatalf("default config: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("nothing written: %v", err)
	}
}

// Load must apply the defaults only a subsystem knows: the LLM preset's
// endpoint and the device profile's display name.
func TestLoadAppliesSubsystemDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	raw := `{
  "address": "AA:BB:CC:DD:EE:FF",
  "active_device": "rc003",
  "devices": [{"id": "rc003", "address": "AA:BB:CC:DD:EE:FF", "profile": "rc003", "enabled": true}],
  "llm": {"provider": "minimax"}
}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.LLM.BaseURL == "" || cfg.LLM.Model == "" {
		t.Fatalf("llm preset not applied: %+v", cfg.LLM)
	}
	if cfg.Devices[0].Name == "" || cfg.Devices[0].Name == "rc003" {
		t.Fatalf("device profile name not applied: %+v", cfg.Devices[0])
	}
}

func TestLoadRejectsSemanticallyBrokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	raw := `{
  "address": "AA:BB:CC:DD:EE:FF",
  "active_device": "rc003",
  "devices": [{"id": "rc003", "address": "AA:BB:CC:DD:EE:FF", "profile": "no-such-profile", "enabled": true}]
}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("an unknown device profile must fail the load")
	}
}

func assertNotWritten(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a rejected config was written: %v", err)
	}
}
