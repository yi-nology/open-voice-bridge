package input

import (
	"testing"
	"time"
)

type fakeSource struct {
	id     string
	kind   Kind
	status Status
}

func (f *fakeSource) ID() string     { return f.id }
func (f *fakeSource) Kind() Kind     { return f.kind }
func (f *fakeSource) Status() Status { return f.status }

func newFake(id string, kind Kind, connected, speaking bool) *fakeSource {
	return &fakeSource{id: id, kind: kind, status: Status{ID: id, Kind: kind, Name: id, Connected: connected, Speaking: speaking}}
}

func TestHubRegisterAndSources(t *testing.T) {
	hub := NewHub()
	hub.Register(newFake("phone-b", KindPhone, true, false))
	hub.Register(newFake("remote", KindRemote, true, true))
	hub.Register(newFake("phone-a", KindPhone, false, false))

	got := hub.Sources()
	if len(got) != 3 {
		t.Fatalf("sources = %d, want 3", len(got))
	}
	// Sorted by kind then id: remote first, then phone-a, phone-b.
	if got[0].ID != "remote" || got[0].Kind != KindRemote {
		t.Fatalf("first = %+v", got[0])
	}
	if got[1].ID != "phone-a" || got[2].ID != "phone-b" {
		t.Fatalf("phone order = %+v %+v", got[1], got[2])
	}
}

func TestHubAudioArbitration(t *testing.T) {
	hub := NewHub()
	if !hub.AcquireAudio("remote") {
		t.Fatal("first acquire should succeed")
	}
	if hub.AudioOwner() != "remote" {
		t.Fatalf("owner = %q", hub.AudioOwner())
	}
	if hub.AcquireAudio("phone-a") {
		t.Fatal("second source should be busy")
	}
	if !hub.AcquireAudio("remote") {
		t.Fatal("owner re-acquire should succeed")
	}
	hub.ReleaseAudio("phone-a") // releasing a non-owner is a no-op
	if hub.AudioOwner() != "remote" {
		t.Fatalf("non-owner release cleared owner: %q", hub.AudioOwner())
	}
	hub.ReleaseAudio("remote")
	if hub.AudioOwner() != "" {
		t.Fatalf("owner not cleared: %q", hub.AudioOwner())
	}
	if !hub.AcquireAudio("phone-a") {
		t.Fatal("phone should acquire after release")
	}
}

func TestHubUnregisterReleasesAudio(t *testing.T) {
	hub := NewHub()
	hub.Register(newFake("remote", KindRemote, true, false))
	hub.AcquireAudio("remote")
	hub.Unregister("remote")
	if hub.AudioOwner() != "" {
		t.Fatalf("unregister did not release audio: %q", hub.AudioOwner())
	}
	if len(hub.Sources()) != 0 {
		t.Fatalf("source still present: %+v", hub.Sources())
	}
}

func TestStatusReflectsSource(t *testing.T) {
	hub := NewHub()
	src := newFake("remote", KindRemote, true, true)
	src.status.LastActive = time.Unix(1, 0)
	hub.Register(src)
	statuses := hub.Sources()
	if len(statuses) != 1 || !statuses[0].Speaking || !statuses[0].Connected || !statuses[0].LastActive.Equal(time.Unix(1, 0)) {
		t.Fatalf("status = %+v", statuses)
	}
}

func TestHubWatchReportsOwnershipTransitions(t *testing.T) {
	hub := NewHub()
	watch := hub.Watch()
	if state := <-watch; state.Speaking || state.Owner != "" {
		t.Fatalf("fresh hub should report idle, got %+v", state)
	}
	if !hub.AcquireAudio("phone-1") {
		t.Fatal("first acquire should succeed")
	}
	state := <-watch
	if state.Owner != "phone-1" || !state.Speaking {
		t.Fatalf("acquire should publish phone-1 speaking, got %+v", state)
	}
	if hub.AcquireAudio("phone-2") {
		t.Fatal("a second phone must be busy while phone-1 holds the mic")
	}
	select {
	case unexpected := <-watch:
		t.Fatalf("failed acquire must not publish, got %+v", unexpected)
	default:
	}
	hub.ReleaseAudio("phone-1")
	if state = <-watch; state.Speaking || state.Owner != "" {
		t.Fatalf("release should publish idle, got %+v", state)
	}
}

func TestHubRemotePreemptsPhone(t *testing.T) {
	hub := NewHub()
	watch := hub.Watch()
	<-watch
	hub.AcquireAudio("phone-1")
	<-watch
	hub.AcquireAudioForce(RemoteID)
	if owner := hub.AudioOwner(); owner != RemoteID {
		t.Fatalf("owner = %q, want the remote to preempt the phone", owner)
	}
	if hub.AcquireAudio("phone-2") {
		t.Fatal("remote still owns the microphone")
	}
	if state := <-watch; state.Owner != RemoteID || !state.Speaking {
		t.Fatalf("preempt should publish remote speaking, got %+v", state)
	}
	// Releasing on behalf of the wrong source must not free the mic.
	hub.ReleaseAudio("phone-1")
	if hub.AudioOwner() != RemoteID {
		t.Fatal("phone-1 release must not free the remote's claim")
	}
}

func TestHubUnregisterFreesDroppedWatcher(t *testing.T) {
	hub := NewHub()
	hub.AcquireAudio("phone-9")
	hub.Unregister("phone-9")
	if hub.AudioOwner() != "" {
		t.Fatal("unregistering the owner must free the microphone")
	}
}

func TestHubWatchKeepsTheNewestStatePerWatcher(t *testing.T) {
	hub := NewHub()
	first := hub.Watch()
	second := hub.Watch()
	hub.AcquireAudio("phone-1")
	if state := <-first; state.Owner != "phone-1" {
		t.Fatalf("first watcher = %+v", state)
	}
	if state := <-second; state.Owner != "phone-1" {
		t.Fatalf("second watcher = %+v", state)
	}
}
