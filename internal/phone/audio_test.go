package phone

import (
	"encoding/binary"
	"testing"
)

func TestUtteranceBufferDecodesLittleEndianPCM(t *testing.T) {
	b := newUtteranceBuffer(sampleRate)
	pcm := make([]byte, 6)
	first, second, third := int16(1234), int16(-2), int16(32767)
	binary.LittleEndian.PutUint16(pcm[0:2], uint16(first))
	binary.LittleEndian.PutUint16(pcm[2:4], uint16(second))
	binary.LittleEndian.PutUint16(pcm[4:6], uint16(third))

	ok, full := b.append(pcm)
	if !ok || full {
		t.Fatalf("append = (%v, %v), want accepted and not full", ok, full)
	}
	samples := b.take()
	want := []int16{1234, -2, 32767}
	if len(samples) != len(want) {
		t.Fatalf("samples = %d, want %d", len(samples), len(want))
	}
	for i, value := range want {
		if samples[i] != value {
			t.Fatalf("samples[%d] = %d, want %d", i, samples[i], value)
		}
	}
	if b.count() != 0 {
		t.Fatalf("take must reset the buffer, count = %d", b.count())
	}
}

func TestUtteranceBufferRejectsMalformedFrames(t *testing.T) {
	b := newUtteranceBuffer(sampleRate)
	if ok, _ := b.append(nil); ok {
		t.Fatal("empty frame must be rejected")
	}
	if ok, _ := b.append([]byte{0x01}); ok {
		t.Fatal("odd byte count must be rejected")
	}
	oversized := make([]byte, (maxFrameSamples+1)*2)
	if ok, _ := b.append(oversized); ok {
		t.Fatal("frame above maxFrameSamples must be rejected")
	}
	if b.count() != 0 {
		t.Fatalf("rejected frames must not accumulate, count = %d", b.count())
	}
}

func TestUtteranceBufferTruncatesAtCap(t *testing.T) {
	b := newUtteranceBuffer(4)
	frame := make([]byte, 8) // 4 samples
	for i := range frame {
		frame[i] = 0x01
	}
	if ok, full := b.append(frame); !ok || !full {
		t.Fatalf("reaching the cap must report full, got (%v, %v)", ok, full)
	}
	if ok, full := b.append(frame); !ok || !full {
		t.Fatalf("a capped buffer keeps reporting full, got (%v, %v)", ok, full)
	}
	if got := b.count(); got != 4 {
		t.Fatalf("count = %d, want the cap to hold at 4", got)
	}
	if got := len(b.take()); got != 4 {
		t.Fatalf("take returned %d samples, want 4", got)
	}
}

func TestUtteranceBufferIsSafeForConcurrentAppend(t *testing.T) {
	b := newUtteranceBuffer(sampleRate * 10)
	frame := make([]byte, 640) // one 20 ms frame
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			for range 40 {
				b.append(frame)
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
	if got := b.count(); got != 8*40*320 {
		t.Fatalf("count = %d, want %d", got, 8*40*320)
	}
}
