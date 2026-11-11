package agent

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/atvv"
	"github.com/yi-nology/open-voice-bridge/internal/core"
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
	appendDictation(a, core.SampleRate, 0x11)
	a.handleControl([]byte{atvv.OpAudioStop})

	a.handleControl([]byte{atvv.OpAudioStart})
	appendDictation(a, 2*core.SampleRate, 0x22)
	a.handleControl([]byte{atvv.OpAudioStop})

	heard := engine.collect(t, 2)
	short, long := engine.pick(t, heard, core.SampleRate), engine.pick(t, heard, 2*core.SampleRate)
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
	if short.SampleRate != core.SampleRate || long.SampleRate != core.SampleRate {
		t.Fatalf("rate = %d/%d, want %d", short.SampleRate, long.SampleRate, core.SampleRate)
	}
}

// collect reads n utterances however the background requests happen to finish,
// because the pipeline does not promise an ordering between them.
func (e *heardEngine) collect(t *testing.T, n int) []asr.Audio {
	t.Helper()
	out := make([]asr.Audio, 0, n)
	for i := 0; i < n; i++ {
		select {
		case audio := <-e.heard:
			out = append(out, audio)
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d utterances reached ASR", len(out), n)
		}
	}
	return out
}

// pick returns the collected utterance holding want samples.
func (e *heardEngine) pick(t *testing.T, heard []asr.Audio, want int) asr.Audio {
	t.Helper()
	for _, audio := range heard {
		if len(audio.Samples) == want {
			return audio
		}
	}
	sizes := make([]int, 0, len(heard))
	for _, audio := range heard {
		sizes = append(sizes, len(audio.Samples))
	}
	t.Fatalf("no utterance of %d samples; sizes = %v", want, sizes)
	return asr.Audio{}
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
	if len(utterance.Samples) != core.SampleRate {
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
	const filled = core.SampleRate / 2
	appendDictation(a, filled, 0x11)
	a.handleControl([]byte{atvv.OpAudioStop})

	// The request is blocked inside ASR while the next capture runs.
	a.handleControl([]byte{atvv.OpAudioStart})
	appendDictation(a, core.SampleRate, 0x22)
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

// TestDisconnectTranscribesTheOpenCapture: a remote that dies mid-sentence took
// down the audio-stop path with it, but the words were already spoken (DOM-04).
func TestDisconnectTranscribesTheOpenCapture(t *testing.T) {
	hub := input.NewHub()
	engine := newHeardEngine()
	a := newFinalizeAgent(engine)
	a.SetHub(hub)
	a.session = atvv.NewSession()

	a.handleControl([]byte{atvv.OpAudioStart})
	appendDictation(a, core.SampleRate/2, 0x11)
	a.markDisconnected()

	utterance := engine.next(t)
	if len(utterance.Samples) != core.SampleRate/2 {
		t.Fatalf("samples = %d, want the half second captured before the drop", len(utterance.Samples))
	}
	if owner := hub.AudioOwner(); owner != "" {
		t.Fatalf("owner = %q, want the disconnect to free the mic", owner)
	}
}

// TestDisconnectDuringShutdownDropsTheCapture is the other half: at exit no text
// may appear in a desktop that is going away.
func TestDisconnectDuringShutdownDropsTheCapture(t *testing.T) {
	engine := newHeardEngine()
	a := newFinalizeAgent(engine)
	ctx, cancel := context.WithCancel(context.Background())
	a.ctx = ctx
	cancel()
	a.session = atvv.NewSession()

	a.handleControl([]byte{atvv.OpAudioStart})
	appendDictation(a, core.SampleRate/2, 0x11)
	a.markDisconnected()

	engine.assertSilence(t)
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
	appendDictation(a, core.SampleRate/8, 0x01)
	a.handleControl([]byte{atvv.OpAudioStop})

	engine.assertSilence(t)
}
