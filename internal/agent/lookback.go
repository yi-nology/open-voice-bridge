package agent

import (
	"sync"

	"github.com/oekit/concurrency/async"

	"github.com/yi-nology/open-voice-bridge/internal/core"
)

// S8 回眸: an in-memory rolling ring of the desktop microphone, so speech
// that was never captured (no key press, early release, failed ASR) can be
// recovered by an explicit action. The ring never touches disk (SEC-02) and
// is dropped the moment the feature turns off — docs/design/lookback.md.

// sampleRing keeps at most seconds of 16 kHz mono audio in memory.
type sampleRing struct {
	mu   sync.Mutex
	data []int16
	max  int
}

func newSampleRing(seconds int) *sampleRing {
	if seconds < 5 {
		seconds = 5
	}
	if seconds > 60 {
		seconds = 60
	}
	return &sampleRing{max: seconds * core.SampleRate}
}

// Append keeps the newest samples; anything beyond the window is dropped.
func (r *sampleRing) Append(samples []int16) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data = append(r.data, samples...)
	if len(r.data) > r.max {
		r.data = r.data[len(r.data)-r.max:]
	}
}

// Tail copies up to seconds of the newest samples.
func (r *sampleRing) Tail(seconds int) []int16 {
	r.mu.Lock()
	defer r.mu.Unlock()
	want := seconds * core.SampleRate
	if want > len(r.data) {
		want = len(r.data)
	}
	out := make([]int16, want)
	copy(out, r.data[len(r.data)-want:])
	return out
}

// Clear forgets everything — the "关即忘记" half of the privacy model.
func (r *sampleRing) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data = nil
}

// SetLookback turns the recording ring on or off (S8). Disabling clears it
// immediately; seconds bounds the window (5..60).
func (a *Agent) SetLookback(enabled bool, seconds int) {
	a.wakeMu.Lock()
	defer a.wakeMu.Unlock()
	if enabled {
		if a.ring == nil || a.ring.max != seconds*core.SampleRate {
			a.ring = newSampleRing(seconds)
		}
		a.lookbackOn.Store(true)
	} else {
		a.lookbackOn.Store(false)
		if a.ring != nil {
			a.ring.Clear()
		}
	}
	a.updateDesktopLocked()
}

// LookbackState reports the ring state for the UI.
func (a *Agent) LookbackState() (bool, int) {
	a.wakeMu.Lock()
	defer a.wakeMu.Unlock()
	seconds := 30
	if a.ring != nil {
		seconds = a.ring.max / core.SampleRate
	}
	return a.lookbackOn.Load(), seconds
}

// RecoverLookback transcribes the last seconds of the ring through the
// shared chain — the only way ring audio ever leaves the loop. It reports
// false when the ring is off or empty.
func (a *Agent) RecoverLookback(seconds int) bool {
	if !a.lookbackOn.Load() {
		return false
	}
	a.wakeMu.Lock()
	ring := a.ring
	a.wakeMu.Unlock()
	if ring == nil {
		return false
	}
	samples := ring.Tail(seconds)
	if len(samples) < core.MinUtterance(core.SampleRate) {
		return false
	}
	async.GoSafe(func() {
		a.pipeline.Transcribe(a.wakeCtxLocked(), Utterance{
			Samples:    samples,
			SampleRate: core.SampleRate,
			Ref:        SourceRef{Kind: "lookback", Name: "回眸"},
		})
	})
	return true
}

// lookbackTap feeds the desktop mic stream into the ring.
func (a *Agent) lookbackTap(frame []int16) {
	if !a.lookbackOn.Load() {
		return
	}
	a.wakeMu.Lock()
	ring := a.ring
	a.wakeMu.Unlock()
	if ring != nil {
		ring.Append(frame)
	}
}
