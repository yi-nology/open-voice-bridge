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

// PartialWindowSeconds bounds what one live-streaming subtitle tick (S6)
// sends to the recognition engine. The draft line previews the freshest
// speech; sending the whole capture so far re-uploads the same audio once
// per tick (a 30s sentence went out ~13x over the wire) and copies it twice.
const PartialWindowSeconds = 4

// Lookback ring window bounds in seconds (S8). One definition so the config
// validator, the normaliser and the ring agree on what "30" means.
const (
	MinLookbackSeconds     = 5
	MaxLookbackSeconds     = 60
	DefaultLookbackSeconds = 30
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

// PartialSamples is the sample budget for one streaming-subtitle tick at
// rate (S6): the freshest PartialWindowSeconds of the capture in flight.
func PartialSamples(rate int) int {
	if rate <= 0 {
		rate = SampleRate
	}
	return PartialWindowSeconds * rate
}

// Resample returns samples converted from fromRate to toRate by linear
// interpolation. The Google TV / Chromecast voice remote negotiates 8 kHz
// over ATVV while the house geometry and the cloud engines are 16 kHz
// (DOM-01 kept the honest rate through the chain; this bridges the last hop
// so the engines can hear the audio at all).
func Resample(samples []int16, fromRate, toRate int) []int16 {
	if fromRate <= 0 {
		fromRate = SampleRate
	}
	if toRate <= 0 {
		toRate = SampleRate
	}
	if fromRate == toRate || len(samples) == 0 {
		return samples
	}
	outLen := int(float64(len(samples)) * float64(toRate) / float64(fromRate))
	if outLen < 1 {
		outLen = 1
	}
	out := make([]int16, outLen)
	step := float64(fromRate) / float64(toRate)
	last := len(samples) - 1
	for i := range out {
		pos := float64(i) * step
		lo := int(pos)
		if lo >= last {
			out[i] = samples[last]
			continue
		}
		frac := pos - float64(lo)
		v := float64(samples[lo])*(1-frac) + float64(samples[lo+1])*frac
		switch {
		case v > 32767:
			out[i] = 32767
		case v < -32768:
			out[i] = -32768
		default:
			out[i] = int16(v)
		}
	}
	return out
}

// SpeechFloor is the peak amplitude below which a capture carries no speech.
// Cloud engines hallucinate a short line on dead air ("你好。", "1234567890")
// and bill for it, so a capture that never rises above this is dropped rather
// than transcribed.
const SpeechFloor = 300

// CarriesSpeech reports whether any sample is loud enough to be a word. The
// peak is what matters, not the mean: one syllable inside a long silence is
// still speech, while a whole capture of room tone is not.
func CarriesSpeech(samples []int16) bool {
	return Peak(samples) > SpeechFloor
}

// Peak is the largest absolute sample. Diagnostics report it instead of the
// waveform: a level proves the capture carried sound without carrying the
// speech itself (SEC-02).
func Peak(samples []int16) int {
	peak := 0
	for _, sample := range samples {
		value := int(sample)
		if value < 0 {
			value = -value
		}
		if value > peak {
			peak = value
		}
	}
	return peak
}
