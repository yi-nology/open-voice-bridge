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
