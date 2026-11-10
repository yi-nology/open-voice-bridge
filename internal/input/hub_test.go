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
