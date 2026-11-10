package agent

import (
	"testing"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/input"
)

func TestBLEAcquiresAndReleasesMicrophone(t *testing.T) {
	hub := input.NewHub()
	engine := &fakeEngine{result: asr.Result{Text: "你好", Provider: "fake", Latency: 20 * time.Millisecond}}
	a := newFinalizeAgent(engine)
	a.SetHub(hub)

	a.acquireMic()
	if owner := hub.AudioOwner(); owner != input.RemoteID {
		t.Fatalf("owner = %q, want the remote to hold the mic", owner)
	}

	fillDictation(a, 1)
	a.finalize()

	if owner := hub.AudioOwner(); owner != "" {
		t.Fatalf("finalize must release the mic, owner = %q", owner)
	}
	if engine.calls != 1 {
		t.Fatalf("ASR calls = %d, want exactly one for the finalized utterance", engine.calls)
	}
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
