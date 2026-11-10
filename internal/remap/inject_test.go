package remap

import (
	"strconv"
	"testing"
)

func TestInjectAllowed(t *testing.T) {
	allowed := []string{
		"up", "down", "left", "right", "enter", "back", "home",
		"menu", "volumeup", "volumedown", "power", "f5",
	}
	for _, key := range allowed {
		if !InjectAllowed(key) {
			t.Fatalf("expected %q to be allowed", key)
		}
		code, ok := InjectKeyCode(key)
		if !ok || code != SourceCodes[key] {
			t.Fatalf("InjectKeyCode(%q) = (%d,%v), want (%d,true)", key, code, ok, SourceCodes[key])
		}
	}
	rejected := []string{"", "ok", "homepage", "playpause", "compose", "grave", "siri", "nope", "UP", "Enter"}
	for _, key := range rejected {
		if InjectAllowed(key) {
			t.Fatalf("expected %q to be rejected", key)
		}
		if code, ok := InjectKeyCode(key); ok || code != 0 {
			t.Fatalf("InjectKeyCode(%q) = (%d,%v), want (0,false)", key, code, ok)
		}
	}
}

func TestInjectWhitelistIsSubsetOfSourceCodes(t *testing.T) {
	for key := range injectWhitelist {
		if _, ok := SourceCodes[key]; !ok {
			t.Fatalf("whitelisted key %q has no source code", key)
		}
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
