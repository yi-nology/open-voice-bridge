// Package core defines the domain vocabulary every layer shares. It does no
// work: the PCM geometry and duration policy live here so that sources, ASR
// engines and config validation cannot drift apart (R5).
package core

// Audio geometry: mono Int16 at 16 kHz. The phone page resamples to it and
// ATVV negotiates it (codec 0x02); audio that arrives at any other rate is
// still stamped with the rate it actually has (DOM-01), but every default in
// the pipeline is this one constant.
const SampleRate = 16000

// FrameBytes is the default ATVV ADPCM wire-frame length used before a device
// reports its own: 120 bytes of IMA 4-bit data, two samples per byte. What a
// device may report is clamped in the protocol package (DOM-05).
const FrameBytes = 120

// Utterance duration bounds in seconds. The remote, the phone and the config
// validators all enforce the same window with the same default.
const (
	MinUtteranceSeconds     = 5
	MaxUtteranceSeconds     = 60
	DefaultUtteranceSeconds = 30
)

// Cap is the sample budget for an utterance of at most seconds at SampleRate.
func Cap(seconds int) int { return seconds * SampleRate }

// MinUtterance is the shortest capture worth a transcription request: a tap
// shorter than 250 ms has never carried a word. The floor scales with the
// rate the audio actually arrived at (DOM-01); zero means "unknown", which
// falls back to the default geometry.
func MinUtterance(rate int) int {
	if rate <= 0 {
		rate = SampleRate
	}
	return rate / 4
}
