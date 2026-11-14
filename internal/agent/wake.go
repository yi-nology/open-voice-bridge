package agent

import (
	"context"
	"strings"
	"time"
	"unicode"

	"github.com/oekit/concurrency/async"

	"github.com/yi-nology/open-voice-bridge/internal/core"
	"github.com/yi-nology/open-voice-bridge/internal/miccap"
)

// S7 唤醒词: the desktop microphone listens for a wake word, then captures
// what follows as an ordinary dictation — no key press anywhere. Wake
// matching runs on throttled partial recognitions of a rolling window, so
// it works with any ASR engine (a local one keeps the whole path local).

const (
	// wakeCheckEvery throttles wake recognitions while voice is active.
	wakeCheckEvery = 900 * time.Millisecond
	// wakeSilence ends the post-wake dictation after this much quiet.
	wakeSilence = 600 * time.Millisecond
	// wakeWindow is how much trailing audio a wake check looks at.
	wakeWindow = 2 * core.SampleRate
	// wakeSilenceRMS gates out background hiss before a recognition runs.
	wakeSilenceRMS = 420
)

// SetWake turns the wake-word listener on or off (S7). Word defaults to
// 小遥小遥. The desktop mic session is shared with the 回眸 ring (S8): it
// runs while either feature needs it and closes when neither does.
func (a *Agent) SetWake(enabled bool, word string) {
	a.wakeMu.Lock()
	defer a.wakeMu.Unlock()
	if strings.TrimSpace(word) == "" {
		word = "小遥小遥"
	}
	a.wakeWord = word
	a.wakeOn = enabled
	a.wakeErr = nil
	a.updateDesktopLocked()
}

// WakeStatus reports the listener state for the UI.
func (a *Agent) WakeStatus() (bool, error) {
	a.wakeMu.Lock()
	defer a.wakeMu.Unlock()
	return a.wakeOn, a.wakeErr
}

// updateDesktopLocked starts or stops the shared desktop mic session.
func (a *Agent) updateDesktopLocked() {
	want := a.wakeOn || a.lookbackOn.Load()
	if !want {
		a.wakeStopLocked()
		return
	}
	if a.wakeStop != nil {
		return
	}
	source, err := miccap.Start(a.wakeCtxLocked(), "")
	if err != nil {
		log.Warn("desktop mic listener unavailable", "err", err)
		a.wakeErr = err
		return
	}
	a.wakeErr = nil
	a.startDesktopLocked(source)
}

// currentWakeWord reports the wake word while the listener is on.
func (a *Agent) currentWakeWord() string {
	a.wakeMu.Lock()
	defer a.wakeMu.Unlock()
	if !a.wakeOn {
		return ""
	}
	return a.wakeWord
}

func (a *Agent) wakeCtxLocked() context.Context {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ctx == nil {
		return context.Background()
	}
	return a.ctx
}

func (a *Agent) startDesktopLocked(source miccap.FrameSource) {
	ctx, cancel := context.WithCancel(a.wakeCtxLocked())
	a.wakeStop = cancel
	a.wakeSource = source
	async.GoSafe(func() { a.desktopLoop(ctx, source) })
}

func (a *Agent) wakeStopLocked() {
	if a.wakeStop != nil {
		a.wakeStop()
		a.wakeStop = nil
	}
	if a.wakeSource != nil {
		_ = a.wakeSource.Close()
		a.wakeSource = nil
	}
}

// wakeLoop watches the mic: rolling-window wake checks while voice is
// active, then a dictation capture that ends on silence or the time cap.
func (a *Agent) desktopLoop(ctx context.Context, source miccap.FrameSource) {
	defer func() {
		a.wakeMu.Lock()
		defer a.wakeMu.Unlock()
		a.wakeStopLocked()
	}()

	window := make([]int16, 0, wakeWindow)
	wakeHitCh := make(chan struct{}, 1)
	lastCheck := time.Time{}
	var dictation []int16
	var silenceSince time.Time
	var hardStop time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case frame, ok := <-source.Frames():
			if !ok {
				return
			}
			now := time.Now()
			a.lookbackTap(frame)
			word := a.currentWakeWord()
			if word == "" && len(dictation) == 0 {
				continue
			}
			if len(dictation) > 0 {
				a.wakeForward(frame)
				dictation = append(dictation, frame...)
				if rms16(frame) < wakeSilenceRMS {
					if silenceSince.IsZero() {
						silenceSince = now.Add(wakeSilence)
					}
				} else {
					silenceSince = time.Time{}
				}
				done := (!silenceSince.IsZero() && now.After(silenceSince)) ||
					len(dictation) >= a.maxSample || now.After(hardStop)
				if !done {
					continue
				}
				samples := dictation
				dictation = nil
				a.releaseMic()
				async.GoSafe(func() { a.runWakeSpeech(samples) })
				continue
			}
			select {
			case <-wakeHitCh:
				log.Info("wake word matched; capturing")
				a.acquireMic()
				dictation = append(dictation, frame...)
				hardStop = now.Add(15 * time.Second)
				silenceSince = time.Time{}
				continue
			default:
			}
			if rms16(frame) < wakeSilenceRMS {
				continue
			}
			window = append(window, frame...)
			if len(window) > wakeWindow {
				window = window[len(window)-wakeWindow:]
			}
			if now.Sub(lastCheck) < wakeCheckEvery || len(window) < core.SampleRate {
				continue
			}
			lastCheck = now
			snapshot := append([]int16(nil), window...)
			async.GoSafe(func() {
				text, err := a.pipeline.TranscribePartial(ctx, Utterance{Samples: snapshot, SampleRate: core.SampleRate})
				if err != nil || !wakeHit(text, word) {
					return
				}
				select {
				case wakeHitCh <- struct{}{}:
				default:
				}
			})
		}
	}
}

// wakeForward plays the wake capture into the virtual mic so other apps
// hear it exactly like a key-driven capture.
func (a *Agent) wakeForward(frame []int16) {
	a.audioMu.Lock()
	sink := a.audio
	a.audioMu.Unlock()
	if sink != nil {
		if err := sink.Write(frame); err != nil {
			log.Debug("virtual mic write failed during wake capture", "err", err)
		}
	}
}

// runWakeSpeech sends the post-wake capture down the shared chain as its
// own source kind, so history says where the words came from.
func (a *Agent) runWakeSpeech(samples []int16) {
	ctx := a.wakeCtxLocked()
	a.pipeline.Transcribe(ctx, Utterance{
		Samples:    samples,
		SampleRate: core.SampleRate,
		Ref:        SourceRef{Kind: "wake", Name: "唤醒词"},
	})
}

// wakeHit reports whether an ASR result contains the wake word. Matching
// ignores punctuation, spacing and case; homophone spellings are handled
// by setting the word to what the engine actually prints.
func wakeHit(text, word string) bool {
	heard := wakeNormalize(text)
	target := wakeNormalize(word)
	if target == "" || heard == "" {
		return false
	}
	return strings.Contains(heard, target) ||
		strings.Contains(heard, strings.Repeat(target, 2))
}

func wakeNormalize(in string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(in) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// rms16 is the frame's loudness, for the voice-activity gate.
func rms16(samples []int16) int {
	if len(samples) == 0 {
		return 0
	}
	sum := 0
	for _, s := range samples {
		v := int(s)
		sum += v * v
	}
	return int(sqrtInt(sum / len(samples)))
}

func sqrtInt(v int) int {
	if v <= 0 {
		return 0
	}
	x, y := v, (v+1)/2
	for y < x {
		x, y = y, (v/y+y)/2
	}
	return x
}
