// Package atvv implements the Android TV Voice-over-BLE protocol used by the
// Xiaomi Bluetooth Remote 2 Pro (RC003).
package atvv

import (
	"encoding/binary"
	"sync"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/core"
)

const (
	ServiceUUID = "ab5e0001-5a21-4f05-bc7d-af01f617b664"
	TXUUID      = "ab5e0002-5a21-4f05-bc7d-af01f617b664"
	AudioUUID   = "ab5e0003-5a21-4f05-bc7d-af01f617b664"
	ControlUUID = "ab5e0004-5a21-4f05-bc7d-af01f617b664"

	// MaxFrameSize is the ceiling for a device-reported frame. At 16 kHz ADPCM
	// (4:1) 4096 bytes already spans about a second of audio; anything larger
	// would make FrameAccumulator wait that long before emitting one frame, so
	// dictation would silently lose every sample (DOM-05).
	MaxFrameSize = 4096

	// maxPending bounds the accumulator against a device that never sends
	// frame-aligned data, so a stalled sync cannot grow memory without end.
	maxPending = 1 << 16
)

var GetCapabilities = []byte{0x0A, 0x01, 0x00, 0x00, 0x03, 0x03}

const (
	OpAudioStop  = 0x00
	OpAudioStart = 0x04
	OpMicButton  = 0x08
	OpAudioSync  = 0x0A
	OpCaps       = 0x0B
)

const (
	EventCaps          = "caps"
	EventMicButton     = "mic-button"
	EventAudioStart    = "audio-start"
	EventAudioStartDup = "audio-start-dup"
	EventAudioStop     = "audio-stop"
	EventAudioSync     = "audio-sync"
	EventUnknown       = "unknown"
)

type Caps struct {
	Version       int
	Codecs        int
	Interaction   int
	FrameSize     int
	SelectedCodec int
	SampleRate    int
}

func ParseCaps(data []byte) (Caps, bool) {
	if len(data) < 7 || data[0] != OpCaps {
		return Caps{}, false
	}
	version := int(data[1])<<8 | int(data[2])
	var codecs, interaction int
	if version >= 0x0100 {
		codecs = int(data[3])
		interaction = int(data[4])
		if codecs == 0 && len(data) >= 9 && data[4]&0x03 != 0 {
			codecs = int(data[4])
			interaction = 0x03
		}
	} else {
		if len(data) < 9 {
			return Caps{}, false
		}
		codecs = int(data[4])
	}
	frameSize := int(data[5])<<8 | int(data[6])
	if frameSize <= 0 || frameSize > MaxFrameSize {
		// The field is a 16-bit device value; an unusable size must not be
		// trusted, or the accumulator waits for a frame that never fills
		// (DOM-05).
		frameSize = core.FrameBytes
	}
	selected := 0x01
	rate := 8000
	if codecs&0x02 != 0 {
		selected = 0x02
		rate = core.SampleRate
	}
	return Caps{
		Version:       version,
		Codecs:        codecs,
		Interaction:   interaction,
		FrameSize:     frameSize,
		SelectedCodec: selected,
		SampleRate:    rate,
	}, true
}

var stepTable = [89]int{
	7, 8, 9, 10, 11, 12, 13, 14, 16, 17, 19, 21, 23, 25, 28, 31,
	34, 37, 41, 45, 50, 55, 60, 66, 73, 80, 88, 97, 107, 118, 130,
	143, 157, 173, 190, 209, 230, 253, 279, 307, 337, 371, 408, 449,
	494, 544, 598, 658, 724, 796, 876, 963, 1060, 1166, 1282, 1411,
	1552, 1707, 1878, 2066, 2272, 2499, 2749, 3024, 3327, 3660, 4026,
	4428, 4871, 5358, 5894, 6484, 7132, 7845, 8630, 9493, 10442,
	11487, 12635, 13899, 15289, 16818, 18500, 20350, 22385, 24623,
	27086, 29794, 32767,
}

var indexTable = [8]int{-1, -1, -1, -1, 2, 4, 6, 8}

// Decoder is a standard IMA/DVI 4-bit ADPCM decoder (two samples per byte,
// high nibble first), matching the RC003 firmware.
type Decoder struct {
	predictor int
	stepIndex int
}

func (d *Decoder) Reset(predictor, stepIndex int) {
	if predictor < -32768 {
		predictor = -32768
	}
	if predictor > 32767 {
		predictor = 32767
	}
	if stepIndex < 0 {
		stepIndex = 0
	}
	if stepIndex > 88 {
		stepIndex = 88
	}
	d.predictor = predictor
	d.stepIndex = stepIndex
}

func (d *Decoder) Decode(data []byte) []int16 {
	out := make([]int16, 0, len(data)*2)
	for _, b := range data {
		out = append(out, d.nibble(int(b>>4)))
		out = append(out, d.nibble(int(b&0x0F)))
	}
	return out
}

func (d *Decoder) nibble(n int) int16 {
	step := stepTable[d.stepIndex]
	diff := step >> 3
	if n&1 != 0 {
		diff += step >> 2
	}
	if n&2 != 0 {
		diff += step >> 1
	}
	if n&4 != 0 {
		diff += step
	}
	if n&8 != 0 {
		d.predictor -= diff
	} else {
		d.predictor += diff
	}
	if d.predictor < -32768 {
		d.predictor = -32768
	}
	if d.predictor > 32767 {
		d.predictor = 32767
	}
	d.stepIndex += indexTable[n&7]
	if d.stepIndex < 0 {
		d.stepIndex = 0
	}
	if d.stepIndex > 88 {
		d.stepIndex = 88
	}
	return int16(d.predictor)
}

// Smooth applies the 3-tap filter used by the reference implementations.
func Smooth(samples []int16) []int16 {
	if len(samples) < 3 {
		return samples
	}
	out := make([]int16, len(samples))
	copy(out, samples)
	for i := 1; i < len(samples)-1; i++ {
		v := (int(samples[i-1]) + 2*int(samples[i]) + int(samples[i+1])) >> 2
		out[i] = int16(v)
	}
	return out
}

// Peak returns the maximum absolute sample value.
func Peak(samples []int16) int {
	peak := 0
	for _, s := range samples {
		v := int(s)
		if v < 0 {
			v = -v
		}
		if v > peak {
			peak = v
		}
	}
	return peak
}

type FrameAccumulator struct {
	pending []byte
	dropped int
}

func (a *FrameAccumulator) Append(data []byte, frameSize int) [][]byte {
	if frameSize <= 0 || frameSize > MaxFrameSize {
		return nil
	}
	a.pending = append(a.pending, data...)
	if len(a.pending) > maxPending {
		// Frame sizes that never align mean the held bytes are not decodable
		// audio; keeping them only delays the next resync.
		a.pending = nil
		a.dropped++
		return nil
	}
	var frames [][]byte
	for len(a.pending) >= frameSize {
		frame := make([]byte, frameSize)
		copy(frame, a.pending[:frameSize])
		frames = append(frames, frame)
		a.pending = a.pending[frameSize:]
	}
	return frames
}

func (a *FrameAccumulator) Reset() {
	a.pending = a.pending[:0]
}

// Dropped counts the resyncs where unframeable bytes had to be discarded.
func (a *FrameAccumulator) Dropped() int { return a.dropped }

// Session holds one ATVV connection's protocol state.
type Session struct {
	mu          sync.Mutex
	decoder     Decoder
	accumulator FrameAccumulator
	frameSize   int
	sampleRate  int
	version     int
	caps        *Caps
	micOpen     bool
	pendingSync *[2]int
	lastStop    time.Time
	lastStart   time.Time
	lastSession int
}

func NewSession() *Session {
	return &Session{frameSize: core.FrameBytes, sampleRate: core.SampleRate}
}

// SampleRate reports the rate the device negotiated in caps. Decoded samples
// must carry it, because stamping a fixed 16 kHz on an 8 kHz codec makes the
// providers hear sped-up speech and writes a lying WAV header (DOM-01).
func (s *Session) SampleRate() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sampleRate
}

func (s *Session) Caps() *Caps {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.caps == nil {
		return nil
	}
	copy := *s.caps
	return &copy
}

func (s *Session) HandleControl(payload []byte) string {
	if len(payload) == 0 {
		return EventUnknown
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch payload[0] {
	case OpCaps:
		caps, ok := ParseCaps(payload)
		if !ok {
			return EventUnknown
		}
		s.caps = &caps
		s.version = caps.Version
		s.frameSize = caps.FrameSize
		s.sampleRate = caps.SampleRate
		return EventCaps
	case OpMicButton:
		return EventMicButton
	case OpAudioStart:
		now := time.Now()
		if s.micOpen && !s.lastStart.IsZero() && now.Sub(s.lastStart) < 500*time.Millisecond {
			return EventAudioStartDup
		}
		s.lastStart = now
		s.decoder.Reset(0, 0)
		s.accumulator.Reset()
		s.pendingSync = nil
		s.micOpen = true
		if len(payload) >= 4 {
			s.lastSession = int(payload[3])
		}
		return EventAudioStart
	case OpAudioStop:
		s.micOpen = false
		s.lastStop = time.Now()
		s.accumulator.Reset()
		return EventAudioStop
	case OpAudioSync:
		if len(payload) >= 7 {
			predictor := int(int16(binary.BigEndian.Uint16(payload[4:6])))
			s.pendingSync = &[2]int{predictor, int(payload[6])}
			return EventAudioSync
		}
	}
	return EventUnknown
}

func (s *Session) HandleAudio(payload []byte) []int16 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.micOpen {
		return nil
	}
	var samples []int16
	for _, frame := range s.accumulator.Append(payload, s.frameSize) {
		if s.pendingSync != nil {
			s.decoder.Reset(s.pendingSync[0], s.pendingSync[1])
			s.pendingSync = nil
		}
		samples = append(samples, Smooth(s.decoder.Decode(frame))...)
	}
	return samples
}

func (s *Session) MicOpenCommand() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.version >= 0x0100 {
		return []byte{0x0C, 0x00}
	}
	return []byte{0x0C, 0x00, 0x00}
}

func (s *Session) MicCloseCommand() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.version >= 0x0100 {
		return []byte{0x0D, byte(s.lastSession & 0xFF)}
	}
	return []byte{0x0D}
}

func (s *Session) MicOpen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.micOpen
}
