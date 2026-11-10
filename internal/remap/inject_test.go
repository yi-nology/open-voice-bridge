package remap

import "testing"

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
