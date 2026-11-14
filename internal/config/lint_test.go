package config

import "testing"

// OPS-10: unknown keys are reported instead of silently dropped.
func TestLintUnknownReportsDroppedKeys(t *testing.T) {
	unknown := LintUnknown([]byte(`{
		"address": "AA:BB:CC:DD:EE:FF",
		"ui_addres": "127.0.0.1:8787",
		"phone": {"enabled": true, "tls_mode": "auto"},
		"lookback_seconds": 30,
		"asr": {"provider": "mimo", "providers": {"x": {"any": 1}}}
	}`))
	want := map[string]bool{"ui_addres": true, "phone.tls_mode": true}
	if len(unknown) != len(want) {
		t.Fatalf("unknown = %v, want %v", unknown, want)
	}
	for _, key := range unknown {
		if !want[key] {
			t.Fatalf("unexpected %q in %v", key, unknown)
		}
	}
	if got := LintUnknown([]byte(`{"address": "x", "phone": {"enabled": true}}`)); len(got) != 0 {
		t.Fatalf("clean config reported %v", got)
	}
}
