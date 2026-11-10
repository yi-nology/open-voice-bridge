package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPhoneDisabledByDefault(t *testing.T) {
	cfg := Default()
	if cfg.Phone.Enabled {
		t.Fatal("phone should be disabled by default")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should validate: %v", err)
	}
}

func TestPhoneValidEnabled(t *testing.T) {
	cfg := Default()
	cfg.Phone = PhoneConfig{Enabled: true, Addr: "0.0.0.0:8788", TLS: "auto", PairCodeTTLSeconds: 300, MaxClients: 4, MaxUtteranceSeconds: 30}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid phone config rejected: %v", err)
	}
}

func TestPhoneValidation(t *testing.T) {
	cases := []struct {
		name string
		seed func(p *PhoneConfig)
		want string
	}{
		{"same port as ui", func(p *PhoneConfig) { p.Addr = "127.0.0.1:8787" }, "different port"},
		{"bad tls", func(p *PhoneConfig) { p.TLS = "weird" }, "phone.tls"},
		{"bad addr", func(p *PhoneConfig) { p.Addr = "1.2.3.4" }, "phone.addr"},
		{"port zero", func(p *PhoneConfig) { p.Addr = "0.0.0.0:0" }, "port"},
		{"ttl too small", func(p *PhoneConfig) { p.PairCodeTTLSeconds = 10 }, "pair_code_ttl"},
		{"clients zero", func(p *PhoneConfig) { p.MaxClients = 0 }, "max_clients"},
		{"utterance too big", func(p *PhoneConfig) { p.MaxUtteranceSeconds = 999 }, "max_utterance"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			cfg.Phone = PhoneConfig{Enabled: true, Addr: "0.0.0.0:8788", TLS: "auto", PairCodeTTLSeconds: 300, MaxClients: 4, MaxUtteranceSeconds: 30}
			tc.seed(&cfg.Phone)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestLoadFillsAndMigratesPhone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	// Build a valid config from defaults, then strip the phone section to
	// simulate a legacy file that predates phone input.
	data, err := json.MarshalIndent(Default(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	delete(raw, "phone")
	legacy, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Phone.Enabled {
		t.Fatal("absent phone must default disabled")
	}
	if cfg.Phone.Addr != "0.0.0.0:8788" || cfg.Phone.TLS != "auto" || cfg.Phone.MaxClients != 4 {
		t.Fatalf("phone defaults not applied: %+v", cfg.Phone)
	}
	saved, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(saved), `"phone"`) {
		t.Fatal("migration should persist a phone section")
	}
}

// SEC-03: pairing codes and long-lived tokens must never travel in cleartext
// on a non-loopback address. phone.tls=off is only allowed on loopback.
func TestPhonePlainTLSNeedsLoopback(t *testing.T) {
	cases := []struct {
		name string
		addr string
		tls  string
		want bool // true = must validate
	}{
		{"off on 0.0.0.0", "0.0.0.0:8788", "off", false},
		{"off on [::]", "[::]:8788", "off", false},
		{"off on LAN IPv4", "192.168.1.5:8788", "off", false},
		{"off on link-local", "169.254.1.1:8788", "off", false},
		{"off on hostname", "voicebox.local:8788", "off", false},
		{"off on 127.0.0.1", "127.0.0.1:8788", "off", true},
		{"off on loopback range", "127.0.0.2:8788", "off", true},
		{"off on localhost", "localhost:8788", "off", true},
		{"off on [::1]", "[::1]:8788", "off", true},
		{"auto on 0.0.0.0", "0.0.0.0:8788", "auto", true},
		{"auto on LAN IPv4", "192.168.1.5:8788", "auto", true},
		{"auto on hostname", "voicebox.local:8788", "auto", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			cfg.Phone = PhoneConfig{Enabled: true, Addr: tc.addr, TLS: tc.tls, PairCodeTTLSeconds: 300, MaxClients: 4, MaxUtteranceSeconds: 30}
			err := cfg.Validate()
			if tc.want {
				if err != nil {
					t.Fatalf("want valid, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("want rejection for non-loopback cleartext phone listener")
			}
			if !strings.Contains(err.Error(), "loopback") {
				t.Fatalf("error should name the loopback rule, got %v", err)
			}
			if !strings.Contains(err.Error(), "tls") {
				t.Fatalf("error should point at the tls remedy, got %v", err)
			}
		})
	}
}

// DOM-08: a disabled phone section must not smuggle invalid values into the
// persisted config; validation runs regardless of Enabled.
func TestPhoneValidatedEvenWhenDisabled(t *testing.T) {
	cases := []struct {
		name string
		seed func(p *PhoneConfig)
		want string
	}{
		{"negative ttl", func(p *PhoneConfig) { p.PairCodeTTLSeconds = -5 }, "pair_code_ttl"},
		{"negative max clients", func(p *PhoneConfig) { p.MaxClients = -1 }, "max_clients"},
		{"negative max utterance", func(p *PhoneConfig) { p.MaxUtteranceSeconds = -30 }, "max_utterance"},
		{"bad tls string", func(p *PhoneConfig) { p.TLS = "weird" }, "phone.tls"},
		{"off on 0.0.0.0", func(p *PhoneConfig) { p.TLS = "off"; p.Addr = "0.0.0.0:8788" }, "loopback"},
		{"ttl above range", func(p *PhoneConfig) { p.PairCodeTTLSeconds = 99999 }, "pair_code_ttl"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			cfg.Phone = PhoneConfig{Enabled: false, Addr: "0.0.0.0:8788", TLS: "auto", PairCodeTTLSeconds: 300, MaxClients: 4, MaxUtteranceSeconds: 30}
			tc.seed(&cfg.Phone)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
	// A disabled section with sane values must keep validating.
	cfg := Default()
	cfg.Phone.Enabled = false
	if err := cfg.Validate(); err != nil {
		t.Fatalf("disabled phone with valid values rejected: %v", err)
	}
}
