package atvv

import "testing"

func TestParseCaps(t *testing.T) {
	caps, ok := ParseCaps([]byte{0x0B, 0x01, 0x00, 0x02, 0x03, 0x00, 0x78, 0x00, 0x00})
	if !ok {
		t.Fatal("caps not parsed")
	}
	if caps.Version != 0x0100 || caps.Codecs != 0x02 || caps.FrameSize != 120 || caps.SampleRate != 16000 {
		t.Fatalf("unexpected caps: %+v", caps)
	}
}

func TestSessionCommands(t *testing.T) {
	session := NewSession()
	if event := session.HandleControl([]byte{0x0B, 0x01, 0x00, 0x02, 0x03, 0x00, 0x78, 0x00, 0x00}); event != EventCaps {
		t.Fatalf("caps event = %s", event)
	}
	if command := session.MicOpenCommand(); len(command) != 2 || command[0] != 0x0C {
		t.Fatalf("mic open = %v", command)
	}
	if event := session.HandleControl([]byte{0x04, 0x00, 0x00, 0x01}); event != EventAudioStart {
		t.Fatalf("audio start = %s", event)
	}
	if event := session.HandleControl([]byte{0x04, 0x00, 0x00, 0x01}); event != EventAudioStartDup {
		t.Fatalf("duplicate start = %s", event)
	}
	if event := session.HandleControl([]byte{0x00}); event != EventAudioStop {
		t.Fatalf("audio stop = %s", event)
	}
}

func TestParseCapsRejectsUnusableFrameSize(t *testing.T) {
	// The frame field is a raw 16-bit device value. Trusting a huge one made the
	// accumulator wait for a frame that never arrives, losing every sample
	// without a trace (DOM-05).
	for _, frame := range []int{0xFFFF, MaxFrameSize + 1} {
		caps, ok := ParseCaps([]byte{0x0B, 0x01, 0x00, 0x02, 0x03, byte(frame >> 8), byte(frame), 0x00, 0x00})
		if !ok {
			t.Fatalf("frame %d: caps not parsed", frame)
		}
		if caps.FrameSize != DefaultFrame {
			t.Fatalf("frame %d: FrameSize = %d, want the default %d", frame, caps.FrameSize, DefaultFrame)
		}
	}
	caps, ok := ParseCaps([]byte{0x0B, 0x01, 0x00, 0x02, 0x03, 0x01, 0x90, 0x00, 0x00})
	if !ok || caps.FrameSize != 400 {
		t.Fatalf("a supported frame must survive: %+v ok=%v", caps, ok)
	}
}

func TestSessionSampleRateFollowsCaps(t *testing.T) {
	// An 8 kHz device must not be reported as 16 kHz: providers and the WAV
	// header take that label as truth (DOM-01).
	session := NewSession()
	if session.SampleRate() != SampleRate {
		t.Fatalf("before caps = %d, want %d", session.SampleRate(), SampleRate)
	}
	payload := []byte{0x0B, 0x01, 0x00, 0x01, 0x00, 0x00, 0x78, 0x00, 0x00}
	if event := session.HandleControl(payload); event != EventCaps {
		t.Fatalf("caps event = %s", event)
	}
	if got := session.SampleRate(); got != 8000 {
		t.Fatalf("after an 8 kHz codec = %d, want 8000", got)
	}
}

func TestFrameAccumulatorResyncsOnUnframeableInput(t *testing.T) {
	var acc FrameAccumulator
	if frames := acc.Append(make([]byte, maxPending+1), DefaultFrame); frames != nil {
		t.Fatalf("oversized input must be discarded, got %d frames", len(frames))
	}
	if acc.Dropped() != 1 {
		t.Fatalf("dropped = %d, want 1", acc.Dropped())
	}
	if len(acc.pending) != 0 {
		t.Fatalf("resync must release the buffer, %d bytes held", len(acc.pending))
	}
	frames := acc.Append(make([]byte, DefaultFrame), DefaultFrame)
	if len(frames) != 1 || acc.Dropped() != 1 {
		t.Fatalf("frames = %d, dropped = %d", len(frames), acc.Dropped())
	}
}

func TestADPCMZero(t *testing.T) {
	decoder := Decoder{}
	decoder.Reset(0, 0)
	samples := decoder.Decode([]byte{0x00, 0x00})
	if len(samples) != 4 {
		t.Fatalf("samples = %d", len(samples))
	}
	if Peak(samples) != 0 {
		t.Fatalf("peak = %d", Peak(samples))
	}
}

func TestPeak(t *testing.T) {
	if Peak([]int16{-100, 20, 99}) != 100 {
		t.Fatal("peak mismatch")
	}
}
