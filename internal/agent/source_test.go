package agent

import (
	"testing"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/gtvkeys"
	"github.com/yi-nology/open-voice-bridge/internal/input"
)

func TestRemoteSourceStatus(t *testing.T) {
	agent := &Agent{
		cfg:       config.Config{Address: "AA:BB:CC:DD:EE:FF"},
		connected: true,
		address:   "AA:BB:CC:DD:EE:FF",
		lastAt:    time.Unix(1700000000, 0),
	}
	source := remoteSource{agent}
	if source.ID() != "remote" || source.Kind() != input.KindRemote {
		t.Fatalf("id/kind = %q/%q", source.ID(), source.Kind())
	}
	status := source.Status()
	if status.Address != "AA:BB:CC:DD:EE:FF" || !status.Connected {
		t.Fatalf("status = %+v", status)
	}
	if status.Speaking {
		t.Fatal("expected idle remote")
	}
	if !status.LastActive.Equal(time.Unix(1700000000, 0)) {
		t.Fatalf("last_active = %v", status.LastActive)
	}

	agent.dictation = []int16{1, 2, 3}
	if !source.Status().Speaking {
		t.Fatal("expected speaking remote while capturing dictation")
	}
}

func TestSetHubRegistersRemote(t *testing.T) {
	agent := &Agent{cfg: config.Config{Address: "AA:BB:CC:DD:EE:FF"}, address: "AA:BB:CC:DD:EE:FF"}
	hub := input.NewHub()
	agent.SetHub(hub)
	statuses := hub.Sources()
	if len(statuses) != 1 || statuses[0].ID != "remote" {
		t.Fatalf("hub sources = %+v", statuses)
	}
	// Setting a nil hub must not panic and leaves the agent usable.
	agent.SetHub(nil)
}

// TestGattKeyReportsReachTheHandler (P3 keys channel): the raw HID reports
// the Google TV family sends over GATT are decoded and forwarded — the
// handler is the composition root's injection seam.
func TestGattKeyReportsReachTheHandler(t *testing.T) {
	a := New(config.Config{MaxUtterance: 30}, &fakeEngine{}, fakeSink{healthy: true})
	var got []gtvkeys.Event
	a.SetGattKeyHandler(func(ev gtvkeys.Event) { got = append(got, ev) })
	a.gattMu.Lock()
	a.gattDecoder = gtvkeys.NewDecoder()
	a.gattMu.Unlock()

	a.handleKeyReport("char1", []byte{0x42, 0x00}) // up press
	a.handleKeyReport("char1", []byte{0x00, 0x00}) // release
	if len(got) != 2 || got[0] != (gtvkeys.Event{Button: "up", Down: true}) ||
		got[1] != (gtvkeys.Event{Button: "up", Down: false}) {
		t.Fatalf("handler saw %+v", got)
	}

	// No handler installed: decode-and-drop, no panic.
	a.SetGattKeyHandler(nil)
	a.handleKeyReport("char1", []byte{0x43, 0x00})
}
