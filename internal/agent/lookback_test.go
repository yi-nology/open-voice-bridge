package agent

import (
	"testing"

	"github.com/yi-nology/open-voice-bridge/internal/config"

	"github.com/yi-nology/open-voice-bridge/internal/core"
)

// S8: the ring keeps only the newest window and Clear forgets it all —
// the "bounded" and "关即忘记" halves of docs/design/lookback.md.
func TestSampleRingKeepsOnlyTheWindow(t *testing.T) {
	ring := newSampleRing(5) // 5s minimum
	want := 5 * core.SampleRate
	one := make([]int16, want)
	one[0] = 1
	ring.Append(one)
	two := make([]int16, want)
	two[0] = 2
	ring.Append(two)
	tail := ring.Tail(5)
	if len(tail) != want {
		t.Fatalf("tail = %d samples, want %d", len(tail), want)
	}
	if tail[0] != 2 {
		t.Fatalf("tail[0] = %d, want the newest window", tail[0])
	}
	ring.Clear()
	if got := ring.Tail(5); len(got) != 0 {
		t.Fatalf("after Clear: %d samples, want 0", len(got))
	}
}

// RecoverLookback refuses when the ring is off — nothing is recognised
// without the explicit action.
func TestRecoverLookbackRefusesWhenOff(t *testing.T) {
	a := New(config.Config{MaxUtterance: 30}, &fakeEngine{}, fakeSink{healthy: true})
	if a.RecoverLookback(30) {
		t.Fatal("recovered from a disabled ring")
	}
	a.SetLookback(true, 30)
	if a.RecoverLookback(30) {
		t.Fatal("recovered from an empty ring")
	}
}
