//go:build linux

package remap

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/config"
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
	text := ConfigText(config.DefaultKeymap)
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

// S3: type()/open() steps defer to the agent; the rewrite hands the daemon
// only synthetic agent(n) keys and reports what each slot means.
func TestParseSeqAgentSteps(t *testing.T) {
	steps, err := ParseSeq(`seq(open(firefox), 200, type(你好，世界), agent(3), enter)`)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 5 {
		t.Fatalf("steps = %d, want 5", len(steps))
	}
	if steps[0].Agent != "open" || steps[0].Arg != "firefox" {
		t.Fatalf("step0 = %+v", steps[0])
	}
	if steps[1].Delay != 200*time.Millisecond {
		t.Fatalf("step1 = %+v", steps[1])
	}
	if steps[2].Agent != "type" || steps[2].Arg != "你好，世界" {
		t.Fatalf("step2 = %+v", steps[2])
	}
	if len(steps[3].Codes) != 1 || steps[3].Codes[0] != AgentActionBase+3 {
		t.Fatalf("step3 = %+v", steps[3])
	}
	if len(steps[4].Codes) != 1 {
		t.Fatalf("step4 = %+v", steps[4])
	}
}

func TestRewriteAgentActionsSharesSlots(t *testing.T) {
	rewritten, actions := RewriteAgentActions(map[string]string{
		"f5":       `seq(open(firefox), 120, type(你好))`,
		"f6":       `seq(type(你好), enter)`,
		"volumeup": "native",
	})
	if rewritten["volumeup"] != "native" {
		t.Fatalf("plain action rewritten: %q", rewritten["volumeup"])
	}
	if rewritten["f5"] != "seq(agent(0), 120, agent(1))" {
		t.Fatalf("f5 = %q", rewritten["f5"])
	}
	if rewritten["f6"] != "seq(agent(1), enter)" {
		t.Fatalf("f6 = %q (type(你好) must share slot 1)", rewritten["f6"])
	}
	if len(actions) != 2 || actions[0].Kind != "open" || actions[1].Arg != "你好" {
		t.Fatalf("actions = %+v", actions)
	}
}
