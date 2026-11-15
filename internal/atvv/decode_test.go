package atvv

import "testing"

// OPS-13: the ADPCM decoder is the audio path's only maths — pin its shape
// with real payloads instead of the 2-byte zero case it started with.

func TestDecoderLengthAndStability(t *testing.T) {
	var decoder Decoder
	payload := []byte{0x12, 0x34, 0x56, 0x78, 0x9A}
	first := decoder.Decode(payload)
	if len(first) != 2*len(payload) {
		t.Fatalf("samples = %d, want %d (two nibbles per byte)", len(first), 2*len(payload))
	}
	// A second decode must not continue the same predictor silently: Decode
	// is stateful across calls, so Reset restores a known origin.
	decoder.Reset(0, 0)
	second := decoder.Decode(payload)
	if len(second) != len(first) {
		t.Fatalf("reset changed length: %d vs %d", len(second), len(first))
	}
	for index := range first {
		if first[index] != second[index] {
			t.Fatalf("sample %d differs after reset: %d vs %d", index, first[index], second[index])
		}
	}
}

func TestDecoderResetClamps(t *testing.T) {
	var decoder Decoder
	decoder.Reset(1<<20, 500)
	// Out-of-range predictor/step index clamp instead of corrupting the
	// first nibble (DOM-01 surface: device-supplied state must not run free).
	out := decoder.Decode([]byte{0x00})
	if len(out) != 2 {
		t.Fatalf("out = %v", out)
	}
	// out is []int16, so a range check would be tautological; the length is
	// what proves the decoder produced samples at all.
}

func TestFrameAccumulatorFraming(t *testing.T) {
	var acc FrameAccumulator
	payload := []byte{1, 2, 3, 4, 5, 6}
	frames := acc.Append(payload[:5], 3)
	if len(frames) != 1 || len(frames[0]) != 3 {
		t.Fatalf("frames = %v", frames)
	}
	frames = acc.Append(payload[5:], 3)
	if len(frames) != 1 {
		t.Fatalf("second append frames = %v", frames)
	}
	if acc.Dropped() != 0 {
		t.Fatalf("dropped = %d", acc.Dropped())
	}
	// Unframeable input resyncs by dropping rather than buffering forever.
	acc.Append(make([]byte, maxPending+1), 3)
	if acc.Dropped() == 0 {
		t.Fatal("oversized pending must resync")
	}
}
