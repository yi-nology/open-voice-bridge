package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

// R5: a second copy of the rate is how the WAV header and the MiMo guard
// drifted apart before (DOM-01). Scan the sources, not just this package.
func TestNoOtherPackageRedefinesTheRate(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", "core":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "16000") {
			t.Errorf("%s hardcodes the sample rate; use internal/core instead (R5)", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}

// Peak is what the diagnostics report instead of the waveform, so it has to
// see the sign the same way on both sides.
func TestPeakReadsBothSigns(t *testing.T) {
	if got := Peak([]int16{0, -900, 400}); got != 900 {
		t.Fatalf("Peak = %d, want 900", got)
	}
	if got := Peak(nil); got != 0 {
		t.Fatalf("Peak(nil) = %d, want 0", got)
	}
}

// TestResampleDoublesThe8kRemote: the Google TV / Chromecast voice remote
// negotiates 8 kHz over ATVV while the house geometry and the cloud engines
// are 16 kHz — Resample bridges the last hop (linear interpolation is plenty
// for speech ASR).
func TestResampleDoublesThe8kRemote(t *testing.T) {
	in := make([]int16, 80)
	for i := range in {
		in[i] = int16(i * 100)
	}
	out := Resample(in, 8000, SampleRate)
	if len(out) != 160 {
		t.Fatalf("len = %d, want 160", len(out))
	}
	if out[0] != 0 {
		t.Fatalf("out[0] = %d, want 0", out[0])
	}
	if out[159] != in[79] {
		t.Fatalf("out[159] = %d, want the last sample %d", out[159], in[79])
	}
	// A value between the endpoints must interpolate, not jump.
	if out[2] <= 0 || out[2] >= 200 {
		t.Fatalf("out[2] = %d, want something between 0 and 200", out[2])
	}
	if same := Resample(in, SampleRate, SampleRate); &same[0] != &in[0] {
		t.Fatal("same-rate resample must return the input as-is")
	}
	if clipped := Resample([]int16{32767, -32768}, 4000, 8000); len(clipped) != 4 {
		t.Fatalf("len = %d, want 4", len(clipped))
	}
}
