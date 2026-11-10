package agent

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/atvv"
	"github.com/yi-nology/open-voice-bridge/internal/input"
)

// heardEngine hands every transcription request to the test, so a test can see
// the exact sample slice the pipeline was given while it is still in flight.
type heardEngine struct {
	heard chan asr.Audio
	gate  chan struct{}
}

func newHeardEngine() *heardEngine {
	return &heardEngine{heard: make(chan asr.Audio)}
}

func (e *heardEngine) Name() string                { return "heard" }
func (e *heardEngine) Local() bool                 { return true }
func (e *heardEngine) Close(context.Context) error { return nil }
func (e *heardEngine) Transcribe(_ context.Context, audio asr.Audio) (asr.Result, error) {
	if e.gate != nil {
		<-e.gate
	}
	e.heard <- audio
	return asr.Result{Text: "ok", Provider: "heard"}, nil
}

func (e *heardEngine) next(t *testing.T) asr.Audio {
	t.Helper()
	select {
	case audio := <-e.heard:
		return audio
	case <-time.After(5 * time.Second):
		t.Fatal("no utterance reached ASR")
		return asr.Audio{}
	}
}

func (e *heardEngine) assertSilence(t *testing.T) {
	t.Helper()
	select {
	case audio := <-e.heard:
		t.Fatalf("unexpected utterance: %d samples", len(audio.Samples))
	case <-time.After(100 * time.Millisecond):
	}
}

// appendDictation mimics what handleAudio does to the live capture buffer.
func appendDictation(a *Agent, count int, value int16) {
	a.dictMu.Lock()
	defer a.dictMu.Unlock()
	for i := 0; i < count; i++ {
		a.dictation = append(a.dictation, value)
	}
}

// TestInterleavedCapturesBecomeSeparateUtterances covers the ordering the
// remote produces when the phone-button is released and pressed again before
// the previous request has been built: each capture must reach ASR with its own
// samples (DOM-02).
func TestInterleavedCapturesBecomeSeparateUtterances(t *testing.T) {
	engine := newHeardEngine()
	a := newFinalizeAgent(engine)
	a.session = atvv.NewSession()

	a.handleControl([]byte{atvv.OpAudioStart})
	appendDictation(a, atvv.SampleRate, 0x11)
	a.handleControl([]byte{atvv.OpAudioStop})

	a.handleControl([]byte{atvv.OpAudioStart})
	appendDictation(a, 2*atvv.SampleRate, 0x22)
	a.handleControl([]byte{atvv.OpAudioStop})

	short, long := engine.nextBySize(t, atvv.SampleRate), engine.nextBySize(t, 2*atvv.SampleRate)
	engine.assertSilence(t)

	// The reset used to rewind the same backing array and the copy used to run
	// after the next capture had already written into it, which turned the pair
	// into two copies of the second sentence (DOM-02).
	if err := homogeneous(short, 0x11); err != nil {
		t.Fatalf("first capture: %v", err)
	}
	if err := homogeneous(long, 0x22); err != nil {
		t.Fatalf("second capture: %v", err)
	}
	if short.SampleRate != atvv.SampleRate || long.SampleRate != atvv.SampleRate {
		t.Fatalf("rate = %d/%d, want %d", short.SampleRate, long.SampleRate, atvv.SampleRate)
	}
}

// nextBySize waits for the utterance holding want samples, so the test does not
// depend on the order the background requests reach the engine.
func (e *heardEngine) nextBySize(t *testing.T, want int) asr.Audio {
	t.Helper()
	var others []int
	for {
		select {
		case audio := <-e.heard:
			if len(audio.Samples) == want {
				return audio
			}
			others = append(others, len(audio.Samples))
			if len(others) >= 2 {
				t.Fatalf("no %d-sample utterance; saw %v", want, others)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no utterance of %d samples reached ASR; saw %v", want, others)
			return asr.Audio{}
		}
	}
}

func homogeneous(audio asr.Audio, value int16) error {
	for i, sample := range audio.Samples {
		if sample != value {
			return fmt.Errorf("sample %d = %#x, want the %#x this capture recorded", i, sample, value)
		}
	}
	return nil
}

// TestDuplicateStartClosesTheOpenCapture: the device may start a stream while
// one is already open. The buffered sentence must still be transcribed and the
// new capture must start empty, without losing the microphone claim (DOM-03).
func TestDuplicateStartClosesTheOpenCapture(t *testing.T) {
	hub := input.NewHub()
	engine := newHeardEngine()
	a := newFinalizeAgent(engine)
	a.SetHub(hub)
	a.session = atvv.NewSession()

	a.handleControl([]byte{atvv.OpAudioStart})
	fillDictation(a, 1)
	// A second start inside the session window: no stop was ever seen.
	a.handleControl([]byte{atvv.OpAudioStart})

	utterance := engine.next(t)
	if len(utterance.Samples) != atvv.SampleRate {
		t.Fatalf("samples = %d, want the buffered second", len(utterance.Samples))
	}
	if owner := hub.AudioOwner(); owner != input.RemoteID {
		t.Fatalf("owner = %q, want the mic kept for the new capture", owner)
	}

	a.handleControl([]byte{atvv.OpAudioStop})
	engine.assertSilence(t)
	if owner := hub.AudioOwner(); owner != "" {
		t.Fatalf("owner = %q, want a stop to free the mic", owner)
	}
}

// TestDrainedSamplesSurviveTheNextCapture is the aliasing half of DOM-02: the
// reset used to rewind the same backing array, so a fast restart overwrote the
// samples already inside the ASR request.
func TestDrainedSamplesSurviveTheNextCapture(t *testing.T) {
	engine := &heardEngine{heard: make(chan asr.Audio), gate: make(chan struct{})}
	a := newFinalizeAgent(engine)
	a.session = atvv.NewSession()

	a.handleControl([]byte{atvv.OpAudioStart})
	const filled = atvv.SampleRate / 2
	appendDictation(a, filled, 0x11)
	a.handleControl([]byte{atvv.OpAudioStop})

	// The request is blocked inside ASR while the next capture runs.
	a.handleControl([]byte{atvv.OpAudioStart})
	appendDictation(a, atvv.SampleRate, 0x22)
	close(engine.gate)

	utterance := engine.next(t)
	if len(utterance.Samples) != filled {
		t.Fatalf("samples = %d, want the %d captured before the stop", len(utterance.Samples), filled)
	}
	for i, sample := range utterance.Samples {
		if sample != 0x11 {
			t.Fatalf("sample %d = %#x, want the drained capture untouched", i, sample)
		}
	}
}

// TestUtteranceCarriesTheNegotiatedRate closes DOM-01 for the remote path: an
// 8 kHz codec must be labelled 8 kHz so providers and the WAV header agree.
func TestUtteranceCarriesTheNegotiatedRate(t *testing.T) {
	engine := newHeardEngine()
	a := newFinalizeAgent(engine)
	a.session = atvv.NewSession()
	a.handleControl([]byte{0x0B, 0x01, 0x00, 0x01, 0x00, 0x00, 0x78, 0x00, 0x00})

	a.handleControl([]byte{atvv.OpAudioStart})
	// An 8 kHz second is half as many samples; the floor is rate-relative.
	appendDictation(a, 8000, 0x01)
	a.handleControl([]byte{atvv.OpAudioStop})

	utterance := engine.next(t)
	if utterance.SampleRate != 8000 {
		t.Fatalf("rate = %d, want the negotiated 8000", utterance.SampleRate)
	}
}

// TestSubSecondUtteranceIsDropped keeps the 250 ms floor alive after minSamples
// moved from the pipeline onto the utterance.
func TestSubSecondUtteranceIsDropped(t *testing.T) {
	engine := newHeardEngine()
	a := newFinalizeAgent(engine)
	a.session = atvv.NewSession()

	a.handleControl([]byte{atvv.OpAudioStart})
	appendDictation(a, atvv.SampleRate/8, 0x01)
	a.handleControl([]byte{atvv.OpAudioStop})

	engine.assertSilence(t)
}
