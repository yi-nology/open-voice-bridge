package core

import "testing"

// R5: the sample rate, frame length, utterance duration bounds and the
// minimum transcribable duration live in exactly one place.
func TestAudioGeometry(t *testing.T) {
	if SampleRate != 16000 {
		t.Fatalf("SampleRate = %d, want 16000", SampleRate)
	}
	if FrameBytes != 120 {
		t.Fatalf("FrameBytes = %d, want 120", FrameBytes)
	}
}

func TestMinUtteranceScalesWithRate(t *testing.T) {
	cases := []struct {
		name string
		rate int
		want int
	}{
		{"16k", SampleRate, SampleRate / 4},
		{"8k negotiated", 8000, 2000},
		{"zero falls back to 16k", 0, SampleRate / 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MinUtterance(tc.rate); got != tc.want {
				t.Fatalf("MinUtterance(%d) = %d, want %d", tc.rate, got, tc.want)
			}
		})
	}
}

func TestCap(t *testing.T) {
	if got := Cap(30); got != 30*SampleRate {
		t.Fatalf("Cap(30) = %d, want %d", got, 30*SampleRate)
	}
	if got := Cap(0); got != 0 {
		t.Fatalf("Cap(0) = %d, want 0", got)
	}
}

func TestDurationBoundsBracketTheDefault(t *testing.T) {
	if MinUtteranceSeconds < 1 {
		t.Fatalf("MinUtteranceSeconds = %d, want >= 1", MinUtteranceSeconds)
	}
	if MaxUtteranceSeconds <= MinUtteranceSeconds {
		t.Fatalf("MaxUtteranceSeconds = %d, want > %d", MaxUtteranceSeconds, MinUtteranceSeconds)
	}
	if DefaultUtteranceSeconds < MinUtteranceSeconds || DefaultUtteranceSeconds > MaxUtteranceSeconds {
		t.Fatalf("DefaultUtteranceSeconds = %d, want within %d..%d",
			DefaultUtteranceSeconds, MinUtteranceSeconds, MaxUtteranceSeconds)
	}
}
