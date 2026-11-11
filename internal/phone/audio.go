package phone

import (
	"encoding/binary"
	"sync"
)

// maxFrameSamples bounds a single binary frame. 3840 samples is 240 ms, well
// above the 20 ms the page sends, so it only catches runaway or hostile
// clients.
const maxFrameSamples = 3840

// utteranceBuffer accumulates one hold-to-talk utterance for one connection.
type utteranceBuffer struct {
	mu         sync.Mutex
	samples    []int16
	maxSamples int
}

func newUtteranceBuffer(maxSamples int) *utteranceBuffer {
	return &utteranceBuffer{
		samples:    make([]int16, 0, maxSamples),
		maxSamples: maxSamples,
	}
}

// append decodes one little-endian Int16 frame into the buffer. ok is false
// for malformed frames, which are dropped rather than trusted; full reports
// the utterance has reached its cap so the caller can end it.
func (b *utteranceBuffer) append(pcm []byte) (ok, full bool) {
	if len(pcm) == 0 || len(pcm)%2 != 0 || len(pcm)/2 > maxFrameSamples {
		return false, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	room := b.maxSamples - len(b.samples)
	if room <= 0 {
		return true, true
	}
	kept := len(pcm) / 2
	if kept > room {
		kept = room
	}
	for index := 0; index < kept; index++ {
		b.samples = append(b.samples, int16(binary.LittleEndian.Uint16(pcm[index*2:])))
	}
	return true, len(b.samples) >= b.maxSamples
}

// take hands the utterance to the speech chain and clears the buffer. The
// caller owns the returned slice: it is a copy, so late frames cannot mutate
// audio already inside ASR.
func (b *utteranceBuffer) take() []int16 {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]int16, len(b.samples))
	copy(out, b.samples)
	b.samples = b.samples[:0]
	return out
}

func (b *utteranceBuffer) reset() {
	b.mu.Lock()
	b.samples = b.samples[:0]
	b.mu.Unlock()
}

func (b *utteranceBuffer) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.samples)
}
