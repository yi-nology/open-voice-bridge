package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yi-nology/open-voice-bridge/internal/asr"
)

func TestLoadCreatesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("default config not written: %v", err)
	}
	if cfg.Keymap.Profile != "coding" {
		t.Fatalf("profile = %q, want coding", cfg.Keymap.Profile)
	}
	for _, name := range []string{"coding", "media", "minimal"} {
		if _, ok := cfg.Keymap.Profiles[name]; !ok {
			t.Fatalf("missing builtin profile %q", name)
		}
	}
	if cfg.ASR.Provider != "mimo" {
		t.Fatalf("asr provider = %q, want mimo", cfg.ASR.Provider)
	}
}

func TestLoadMigratesLegacyConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	legacy := `{
  "address": "AA:BB:CC:DD:EE:99",
  "ui_addr": "127.0.0.1:8787",
  "asr_model": "mimo-v2.5-asr",
  "asr_language": "auto"
}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg.Keymap.Profile == "" || len(cfg.Keymap.Profiles) == 0 {
		t.Fatalf("keymap not migrated: %+v", cfg.Keymap)
	}
	if cfg.ASR.Provider == "" {
		t.Fatalf("asr not migrated: %+v", cfg.ASR)
	}
	if string(data) == legacy {
		t.Fatal("legacy config was not rewritten")
	}
}

func TestSaveKeymapProfileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Default()
	cfg.Keymap.Profiles["studio"] = map[string]string{
		"power":      "esc",
		"f5":         "f24",
		"up":         "native",
		"down":       "native",
		"left":       "native",
		"right":      "native",
		"enter":      "native",
		"back":       "esc",
		"home":       "native",
		"compose":    "native",
		"grave":      "noop",
		"volumeup":   "native",
		"volumedown": "native",
	}
	cfg.Keymap.Profile = "studio"
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Keymap.Profile != "studio" {
		t.Fatalf("profile = %q, want studio", loaded.Keymap.Profile)
	}
	if loaded.Keymap.Profiles["studio"]["power"] != "esc" {
		t.Fatalf("profile content lost: %+v", loaded.Keymap.Profiles["studio"])
	}
}

func TestValidateAllowsLocalOnlyASR(t *testing.T) {
	cfg := Default()
	cfg.APIKeyFile = ""
	cfg.ASR = asr.Config{
		Provider: "local",
		Providers: map[string]asr.ProviderConfig{
			"local": {Type: "command", Binary: "/bin/echo"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("local-only config rejected: %v", err)
	}
}

func TestSaveLocalOnlyKeepsProviders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Default()
	cfg.APIKeyFile = ""
	cfg.ASR = asr.Config{
		Provider: "local",
		Providers: map[string]asr.ProviderConfig{
			"local": {Type: "command", Binary: "/bin/echo"},
		},
	}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := loaded.ASR.Providers["mimo"]; ok {
		t.Fatalf("unexpected synthesized mimo provider: %+v", loaded.ASR.Providers)
	}
	if loaded.ASR.Provider != "local" {
		t.Fatalf("provider = %q", loaded.ASR.Provider)
	}
}

func TestDefaultDevices(t *testing.T) {
	cfg := Default()
	if len(cfg.Devices) != 1 || cfg.Devices[0].ID != "rc003" {
		t.Fatalf("devices = %+v", cfg.Devices)
	}
	if cfg.ActiveDevice != "rc003" || cfg.Address != cfg.Devices[0].Address {
		t.Fatalf("active=%q address=%q", cfg.ActiveDevice, cfg.Address)
	}
}

func TestLegacyConfigMigratesDevices(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	legacy := `{
  "address": "AA:BB:CC:DD:EE:99",
  "ui_addr": "127.0.0.1:8787",
  "max_utterance_seconds": 30,
  "api_key_file": "/tmp/key",
  "helper_socket": "/run/xiaomi-remote-helper.sock",
  "asr": {"provider":"local","providers":{"local":{"type":"command","binary":"/bin/echo"}}},
  "keymap": {"profile":"coding","profiles":{"coding":{}}}
}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Devices) != 1 || cfg.Devices[0].Address != "AA:BB:CC:DD:EE:99" {
		t.Fatalf("devices = %+v", cfg.Devices)
	}
}

func TestSaveDevicesRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Default()
	cfg.Devices = append(cfg.Devices, DeviceConfig{
		ID:      "rc003-2",
		Name:    "Second",
		Address: "AA:BB:CC:DD:EE:01",
		Profile: "rc003",
		Enabled: true,
	})
	cfg.ActiveDevice = "rc003-2"
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Devices) != 2 || loaded.ActiveDevice != "rc003-2" || loaded.Address != "AA:BB:CC:DD:EE:01" {
		t.Fatalf("loaded = %+v", loaded)
	}
}

func TestDefaultLLM(t *testing.T) {
	cfg := Default()
	if cfg.LLM.Enabled || cfg.LLM.Provider != "minimax" {
		t.Fatalf("llm = %+v", cfg.LLM)
	}
	if cfg.LLM.BaseURL == "" || cfg.LLM.Model == "" {
		t.Fatalf("llm defaults = %+v", cfg.LLM)
	}
}

func TestLLMRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Default()
	cfg.LLM.Enabled = true
	cfg.LLM.Provider = "volcengine"
	cfg.LLM.BaseURL = "https://ark.cn-beijing.volces.com/api/v3"
	cfg.LLM.Model = "ep-test"
	cfg.LLM.Mode = "format"
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !loaded.LLM.Enabled || loaded.LLM.Provider != "volcengine" || loaded.LLM.Mode != "format" {
		t.Fatalf("llm = %+v", loaded.LLM)
	}
}
