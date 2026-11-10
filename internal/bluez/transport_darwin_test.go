//go:build darwin && cgo

package bluez

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPumpRoutesFramesAndEvents(t *testing.T) {
	transport := New("AA:BB:CC:DD:EE:FF")

	var audio [][]byte
	var control [][]byte
	transport.onAudio = func(payload []byte) { audio = append(audio, payload) }
	transport.onControl = func(payload []byte) { control = append(control, payload) }

	done := make(chan struct{})
	go func() {
		transport.pump()
		close(done)
	}()

	transport.audioFrames <- []byte{0x01, 0x02}
	transport.ctlFrames <- []byte{0x0A}
	transport.events <- bleEvent{kind: eventSubscribed}
	transport.events <- bleEvent{kind: eventDisconnected, message: "gone"}

	select {
	case event := <-transport.results:
		if event.kind != eventSubscribed {
			t.Fatalf("routed event %d, want subscribed", event.kind)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("completion event not routed")
	}

	select {
	case <-transport.disconnected:
	case <-time.After(2 * time.Second):
		t.Fatal("disconnected channel not closed")
	}

	_ = transport.Close(t.Context())

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pump did not stop")
	}

	if len(audio) != 1 || len(audio[0]) != 2 {
		t.Errorf("audio frames = %v", audio)
	}
	if len(control) != 1 || len(control[0]) != 1 {
		t.Errorf("control frames = %v", control)
	}
}

func TestDisconnectedClosesOnlyOnce(t *testing.T) {
	transport := New("AA:BB:CC:DD:EE:FF")
	go transport.pump()

	for range 2 {
		transport.events <- bleEvent{kind: eventDisconnected}
	}
	select {
	case <-transport.disconnected:
	case <-time.After(2 * time.Second):
		t.Fatal("disconnected channel not closed")
	}
	// A second close from Close must not panic on the already-closed channel.
	if err := transport.Close(t.Context()); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestBLEIDMapRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	address := "AA:BB:CC:DD:EE:FF"
	if got := loadBLEID(address); got != "" {
		t.Fatalf("pre-save load = %q, want empty", got)
	}

	saveBLEID(address, "6E1E5B10-0000-0000-0000-000000000001")
	if got := loadBLEID(address); got != "6E1E5B10-0000-0000-0000-000000000001" {
		t.Fatalf("load = %q", got)
	}
	if got := loadBLEID("FF:FF:FF:FF:FF:FF"); got != "" {
		t.Fatalf("unknown address load = %q, want empty", got)
	}

	path := filepath.Join(home, ".local", "state", "xiaomi-remote", "ble-id-map.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat map: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("map permissions = %o, want 600", perm)
	}
}
