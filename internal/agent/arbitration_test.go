package agent

import (
	"context"
	"testing"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/atvv"
	"github.com/yi-nology/open-voice-bridge/internal/input"
)

func TestBLEAudioStopReleasesTheMicrophoneBeforeASR(t *testing.T) {
	hub := input.NewHub()
	engine := &gatedEngine{
		result:  asr.Result{Text: "你好", Provider: "gated", Latency: 20 * time.Millisecond},
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	a := newFinalizeAgent(engine)
	a.SetHub(hub)
	a.session = atvv.NewSession()

	a.handleControl([]byte{atvv.OpAudioStart})
	if owner := hub.AudioOwner(); owner != input.RemoteID {
		t.Fatalf("audio start must claim the mic, owner = %q", owner)
	}
	fillDictation(a, 1)

	a.handleControl([]byte{atvv.OpAudioStop})
	if owner := hub.AudioOwner(); owner != "" {
		t.Fatalf("audio stop must free the mic, owner = %q", owner)
	}
	if !hub.AcquireAudio("phone-1") {
		t.Fatal("a phone must be able to claim the mic while the remote transcribes")
	}

	close(engine.release)
	select {
	case <-engine.started:
	case <-time.After(5 * time.Second):
		t.Fatal("audio stop must still transcribe the buffered utterance")
	}
}

// gatedEngine blocks inside Transcribe so a test can observe the microphone
// state during the recognition window.
type gatedEngine struct {
	result  asr.Result
	started chan struct{}
	release chan struct{}
}

func (g *gatedEngine) Name() string                { return "gated" }
func (g *gatedEngine) Local() bool                 { return true }
func (g *gatedEngine) Close(context.Context) error { return nil }
func (g *gatedEngine) Transcribe(context.Context, asr.Audio) (asr.Result, error) {
	g.started <- struct{}{}
	<-g.release
	return g.result, nil
}

func TestBLEPreemptsPhoneAndSurvivesWithoutHub(t *testing.T) {
	hub := input.NewHub()
	hub.AcquireAudio("phone-1")
	a := newFinalizeAgent(&fakeEngine{})
	a.SetHub(hub)
	a.acquireMic()
	if hub.AudioOwner() != input.RemoteID {
		t.Fatalf("the remote must win the microphone, owner = %q", hub.AudioOwner())
	}

	// No hub at all: dictation must behave exactly as before Phase 2.
	bare := newFinalizeAgent(&fakeEngine{})
	bare.acquireMic()
	bare.finalize()
	bare.releaseMic()
}

func TestBLEDisconnectReleasesMicrophone(t *testing.T) {
	hub := input.NewHub()
	a := newFinalizeAgent(&fakeEngine{})
	a.SetHub(hub)
	a.acquireMic()

	// A capture cut short by a disconnect never reaches finalize.
	a.markDisconnected()

	if owner := hub.AudioOwner(); owner != "" {
		t.Fatalf("owner = %q, want a disconnect to free the mic for phones", owner)
	}
	if !hub.AcquireAudio("phone-1") {
		t.Fatal("a phone must be able to claim the mic after the remote drops")
	}
}
