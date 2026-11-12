package remap

import (
	"strconv"
	"testing"
	"time"
)

func TestInjectAllowed(t *testing.T) {
	allowed := []string{
		"up", "down", "left", "right", "enter", "back", "home",
		"menu", "volumeup", "volumedown", "f5",
	}
	for _, allowPower := range []bool{false, true} {
		for _, key := range allowed {
			if !InjectAllowed(key, allowPower) {
				t.Fatalf("expected %q to be allowed (allowPower=%v)", key, allowPower)
			}
			code, ok := InjectKeyCode(key, allowPower)
			if !ok || code != SourceCodes[key] {
				t.Fatalf("InjectKeyCode(%q) = (%d,%v), want (%d,true)", key, code, ok, SourceCodes[key])
			}
		}
	}
	rejected := []string{"", "ok", "homepage", "playpause", "compose", "grave", "siri", "nope", "UP", "Enter"}
	for _, allowPower := range []bool{false, true} {
		for _, key := range rejected {
			if InjectAllowed(key, allowPower) {
				t.Fatalf("expected %q to be rejected (allowPower=%v)", key, allowPower)
			}
			if code, ok := InjectKeyCode(key, allowPower); ok || code != 0 {
				t.Fatalf("InjectKeyCode(%q) = (%d,%v), want (0,false)", key, code, ok)
			}
		}
	}
}

// ARC-12: KEY_POWER reaches logind, so injecting it can shut the host down.
// cmdInject is open to any peer that passed the socket group check (auth.go),
// which makes this whitelist the only boundary — power must not be on it by
// default, and the opt-in must be the only way through.
func TestPowerKeyNeedsAnExplicitOptIn(t *testing.T) {
	if InjectAllowed("power", false) {
		t.Fatal("power is injectable by default; a paired phone can power off the host")
	}
	if code, ok := InjectKeyCode("power", false); ok || code != 0 {
		t.Fatalf("InjectKeyCode(power, false) = (%d,%v), want (0,false)", code, ok)
	}
	if !InjectAllowed("power", true) {
		t.Fatal("the opt-in must let power through")
	}
	code, ok := InjectKeyCode("power", true)
	if !ok || code != SourceCodes["power"] {
		t.Fatalf("InjectKeyCode(power, true) = (%d,%v), want (%d,true)", code, ok, SourceCodes["power"])
	}
}

func TestInjectWhitelistIsSubsetOfSourceCodes(t *testing.T) {
	for key := range injectWhitelist {
		if _, ok := SourceCodes[key]; !ok {
			t.Fatalf("whitelisted key %q has no source code", key)
		}
	}
	if _, ok := SourceCodes[powerInjectKey]; !ok {
		t.Fatalf("gated key %q has no source code", powerInjectKey)
	}
	if injectWhitelist[powerInjectKey] {
		t.Fatalf("%q must stay out of the unconditional whitelist", powerInjectKey)
	}
}

// Common phrases (SayAll 对标): phrase1..phrase8 are first-class mapping
// actions backed by synthetic F13–F20 key-downs the agent listens for.
func TestPhraseActions(t *testing.T) {
	if err := Validate(map[string]string{"menu": "phrase1", "ok": "phrase8"}); err != nil {
		t.Fatalf("phrase actions must validate: %v", err)
	}
	if err := Validate(map[string]string{"menu": "phrase9"}); err == nil {
		t.Fatal("phrase9 is out of range and must be rejected")
	}
	if err := Validate(map[string]string{"menu": "phrase0"}); err == nil {
		t.Fatal("phrase0 is out of range and must be rejected")
	}
	if err := Validate(map[string]string{"menu": "phrase"}); err == nil {
		t.Fatal("bare phrase without a slot must be rejected")
	}
	for slot := 1; slot <= 8; slot++ {
		code, ok := ActionKeyCode("phrase" + strconv.Itoa(slot))
		if !ok {
			t.Fatalf("phrase%d has no key code", slot)
		}
		// F13 = 183 through F20 = 190: the agent's watcher keys off these.
		if code != 182+slot {
			t.Fatalf("phrase%d code = %d, want %d (F%d)", slot, code, 182+slot, 12+slot)
		}
	}
	if _, ok := ActionKeyCode("phrase9"); ok {
		t.Fatal("phrase9 must not resolve to a key code")
	}
	merged := MergeDefaults(map[string]string{"menu": "phrase3"})
	if merged["menu"] != "phrase3" {
		t.Fatalf("merged menu = %q, want phrase3", merged["menu"])
	}
}

// 组合动作（SayAll「一键做更多」）: seq(...) plays ordered key steps with
// optional delays. Tokens use the existing action vocabulary; bare integers
// are pauses in milliseconds.
func TestParseSeqSteps(t *testing.T) {
	steps, err := ParseSeq("seq(up, 40, macro(leftctrl+c), enter)")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(steps) != 4 {
		t.Fatalf("steps = %d, want 4", len(steps))
	}
	if steps[1].Delay != 40*time.Millisecond {
		t.Fatalf("delay step = %+v", steps[1])
	}
	up := SourceCodes["up"]
	if len(steps[0].Codes) != 1 || steps[0].Codes[0] != up {
		t.Fatalf("first step = %+v, want a tap of up (%d)", steps[0], up)
	}
	if len(steps[2].Codes) != 2 {
		t.Fatalf("macro step = %+v", steps[2])
	}
	if _, err := ParseSeq("seq(nosuchkey)"); err == nil {
		t.Fatal("unknown token must fail")
	}
	if _, err := ParseSeq("seq()"); err == nil {
		t.Fatal("empty seq must fail")
	}
	if err := Validate(map[string]string{"home": "seq(up,enter)"}); err != nil {
		t.Fatalf("Validate must accept seq: %v", err)
	}
	if err := Validate(map[string]string{"home": "seq(nosuchkey)"}); err == nil {
		t.Fatal("Validate must reject a bad seq")
	}
}

// Per-app keymap matching is a plain case-insensitive substring test on the
// focused window class (X11 WM_CLASS res_class).
func TestMatchClass(t *testing.T) {
	cases := []struct {
		class, pattern string
		want           bool
	}{
		{"Navigator/firefox", "firefox", true},
		{"Code", "code", true},
		{"kitty", "firefox", false},
		{"anything", "", false},
	}
	for _, tc := range cases {
		if got := MatchClass(tc.class, tc.pattern); got != tc.want {
			t.Fatalf("MatchClass(%q,%q) = %v", tc.class, tc.pattern, got)
		}
	}
}
