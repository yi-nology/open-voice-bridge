package agent

import (
	"testing"
	"time"
)

// R6: the agent's snapshot is a typed view. It used to be a map[string]any that
// three other layers re-asserted key by key, so a renamed key or a changed value
// type became a silent zero downstream instead of a compile error (ARC-05).
func TestStatusIsATypedView(t *testing.T) {
	at := time.Date(2026, 11, 11, 9, 30, 0, 0, time.UTC)
	a := &Agent{
		connected:   true,
		ready:       true,
		lastText:    "你好",
		lastAt:      at,
		lastLatency: 320 * time.Millisecond,
		audio:       fakeSink{healthy: true},
		asr:         &fakeEngine{},
	}
	got := a.Status()
	if !got.Connected || !got.Ready || !got.MicHealthy {
		t.Fatalf("connected/ready/mic_healthy = %v/%v/%v", got.Connected, got.Ready, got.MicHealthy)
	}
	if got.LastText != "你好" {
		t.Fatalf("last text = %q", got.LastText)
	}
	if !got.LastAt.Equal(at) {
		t.Fatalf("last at = %v, want %v", got.LastAt, at)
	}
	if got.LastLatency != 320*time.Millisecond {
		t.Fatalf("last latency = %v", got.LastLatency)
	}
	if got.ASR != "fake" || !got.ASRLocal {
		t.Fatalf("asr = %q local=%v", got.ASR, got.ASRLocal)
	}
}

// A never-spoken agent must report a zero time, not an absent map key that a
// downstream layer copies as a nil any and serialises as JSON null.
func TestStatusWithoutSpeechKeepsZeroValues(t *testing.T) {
	got := (&Agent{}).Status()
	if !got.LastAt.IsZero() {
		t.Fatalf("last at = %v, want zero", got.LastAt)
	}
	if got.LastText != "" || got.LastLatency != 0 {
		t.Fatalf("last text/latency = %q/%v", got.LastText, got.LastLatency)
	}
	if got.ASR != "unknown" || got.ASRLocal || got.MicHealthy {
		t.Fatalf("asr = %q local=%v mic=%v", got.ASR, got.ASRLocal, got.MicHealthy)
	}
}
