//go:build linux

package remap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yi-nology/open-voice-bridge/internal/device"
)

func TestDefaultMappingValid(t *testing.T) {
	if err := Validate(MergeDefaults(nil)); err != nil {
		t.Fatalf("default mapping: %v", err)
	}
}

func TestMacroParsing(t *testing.T) {
	codes, ok := MacroTokenCodes("macro(leftalt+tab)")
	if !ok || len(codes) != 2 {
		t.Fatalf("macro parse: %v %v", codes, ok)
	}
	if codes[0] != MacroTokens["leftalt"] || codes[1] != MacroTokens["tab"] {
		t.Fatalf("macro codes: %v", codes)
	}
}

func TestInvalidMappings(t *testing.T) {
	cases := []map[string]string{
		{"nope": "esc"},
		{"power": "not-an-action"},
		{"home": "macro(leftalt+unknown)"},
		{"home": "macro()"},
	}
	for _, mapping := range cases {
		if err := Validate(mapping); err == nil {
			t.Fatalf("expected error for %v", mapping)
		}
	}
}

func TestLoadSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mapping.json")
	mapping, err := Load(path)
	if err != nil {
		t.Fatalf("load missing: %v", err)
	}
	if mapping["power"] != "esc" {
		t.Fatalf("default power = %q", mapping["power"])
	}
	mapping["home"] = "macro(leftmeta+d)"
	if err := Save(path, mapping); err != nil {
		t.Fatalf("save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %o", info.Mode().Perm())
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded["home"] != "macro(leftmeta+d)" {
		t.Fatalf("home = %q", reloaded["home"])
	}
}

func TestConfigText(t *testing.T) {
	text := ConfigText(DefaultMapping)
	if text == "" || text[0] != '{' {
		t.Fatalf("config text = %q", text)
	}
}

func TestRemoteBlockMatching(t *testing.T) {
	profile, ok := device.Builtin().Get("rc003")
	if !ok {
		t.Fatal("rc003 profile missing")
	}
	generic := `I: Bus=0005 Vendor=2717 Product=32b8 Version=00a4
N: Name="bluez-hog-device"
H: Handlers=sysrq kbd event12`
	if !matchBlock(profile.Match, generic) {
		t.Fatal("generic bluez-hog-device should match by VID/PID")
	}
	named := `I: Bus=0005 Vendor=2717 Product=32b8
N: Name="小米蓝牙语音遥控器"`
	if !matchBlock(profile.Match, named) {
		t.Fatal("named remote should match")
	}
	other := `I: Bus=0003 Vendor=046d Product=c52b
N: Name="USB Keyboard"`
	if matchBlock(profile.Match, other) {
		t.Fatal("unrelated keyboard must not match")
	}
}

func TestCustomMatcher(t *testing.T) {
	matcher := device.Match{Vendor: "1234", Product: "5678"}
	block := `I: Bus=0005 Vendor=1234 Product=5678
N: Name="Some BLE Remote"
H: Handlers=sysrq kbd event20`
	if !matchBlock(matcher, block) {
		t.Fatal("custom vendor/product should match")
	}
	if matchBlock(matcher, "Vendor=9999 Product=0001") {
		t.Fatal("different vendor/product must not match")
	}
}

func TestGenericKeyMappingValid(t *testing.T) {
	if err := Validate(MergeDefaults(nil)); err != nil {
		t.Fatalf("default mapping: %v", err)
	}
	for _, key := range []string{"ok", "homepage", "menu", "playpause", "nextsong", "previoussong", "mute"} {
		if _, ok := SourceCodes[key]; !ok {
			t.Fatalf("missing generic key %q", key)
		}
	}
}
