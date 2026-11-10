//go:build linux

package bluez

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	testAudioPath   = dbus.ObjectPath("/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF/service0/char0")
	testControlPath = dbus.ObjectPath("/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF/service0/char1")
)

// stubChar answers the one BusObject method the transport calls. The embedded
// nil interface stands in for the rest, so a method the transport must not use
// still panics loudly in a test rather than silently succeeding.
type stubChar struct {
	dbus.BusObject
	mu    sync.Mutex
	calls []string
}

func (s *stubChar) Call(method string, flags dbus.Flags, args ...any) *dbus.Call {
	s.mu.Lock()
	s.calls = append(s.calls, method)
	s.mu.Unlock()
	return &dbus.Call{}
}

func (s *stubChar) called(method string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, got := range s.calls {
		if got == method {
			return true
		}
	}
	return false
}

// newTestTransport builds a Transport in the post-Connect state without a D-Bus
// daemon: the dispatcher channel and the resolved characteristics are installed
// directly, so the tests exercise Subscribe, signal routing and Close.
func newTestTransport(t *testing.T) (*Transport, *stubChar, *stubChar) {
	t.Helper()
	audio, control := &stubChar{}, &stubChar{}
	tr := New("AA:BB:CC:DD:EE:FF")
	tr.links = links{
		audio:       audio,
		control:     control,
		audioPath:   testAudioPath,
		controlPath: testControlPath,
	}
	tr.signals = make(chan *dbus.Signal, 16)
	return tr, audio, control
}

func changed(path dbus.ObjectPath, value []byte) *dbus.Signal {
	return &dbus.Signal{
		Name: "org.freedesktop.DBus.Properties.PropertiesChanged",
		Path: path,
		Body: []any{
			"org.bluez.GattCharacteristic1",
			map[string]dbus.Variant{"Value": dbus.MakeVariant(value)},
			[]string{"Value"},
		},
	}
}

func TestNotificationsRouteToTheSubscribedCallbacks(t *testing.T) {
	tr, audio, control := newTestTransport(t)
	delivered := make(chan string, 8)

	// CON-01: a notification that arrives before Subscribe is dropped, not a nil
	// dereference on the way into the dispatcher.
	tr.handleSignal(changed(testControlPath, []byte{0x03}))
	if err := tr.Subscribe(
		func([]byte) { delivered <- "audio" },
		func([]byte) { delivered <- "control" },
	); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if !audio.called("org.bluez.GattCharacteristic1.StartNotify") ||
		!control.called("org.bluez.GattCharacteristic1.StartNotify") {
		t.Fatal("Subscribe must start notify on both characteristics")
	}

	go tr.loop()
	defer func() { _ = tr.Close(context.Background()) }()
	tr.signals <- changed(testAudioPath, []byte{0x01})
	tr.signals <- changed(testControlPath, []byte{0x02})
	for _, want := range []string{"audio", "control"} {
		select {
		case got := <-delivered:
			if got != want {
				t.Fatalf("delivered %q, want %q", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("no %q notification delivered", want)
		}
	}
}

func TestSubscribeWhileDispatcherRunsIsSafe(t *testing.T) {
	tr, _, _ := newTestTransport(t)
	go tr.loop()
	defer func() { _ = tr.Close(context.Background()) }()

	// The agent's real sequence: Connect starts the dispatcher, Subscribe lands
	// afterwards. Without the callback mutex -race flags this pair.
	var wg sync.WaitGroup
	for i := 0; i < 25; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			tr.signals <- changed(testAudioPath, []byte{0x01})
		}()
		go func() {
			defer wg.Done()
			if err := tr.Subscribe(func([]byte) {}, func([]byte) {}); err != nil {
				t.Errorf("Subscribe: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestCloseIsIdempotent(t *testing.T) {
	tr, _, _ := newTestTransport(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := tr.Close(context.Background()); err != nil {
					t.Errorf("Close: %v", err)
				}
			}()
		}
		wg.Wait()
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent Close hung")
	}
	select {
	case <-tr.done:
	default:
		t.Fatal("Close must close done so the dispatcher stops")
	}
}

func TestPublishRefusesAfterClose(t *testing.T) {
	tr, _, _ := newTestTransport(t)
	if err := tr.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Close may land while a reconnect attempt is still resolving the GATT tree;
	// publishing after it must not hand the agent a dead connection.
	if err := tr.publish(&dbus.Conn{}, tr.links, make(chan *dbus.Signal, 1)); err == nil {
		t.Fatal("publish must refuse once the transport is closed")
	}
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	if tr.conn != nil {
		t.Fatal("a refused publish must not install a connection")
	}
}

func TestSubscribeAndWriteBeforeConnectFail(t *testing.T) {
	tr := New("AA:BB:CC:DD:EE:FF")
	if err := tr.Subscribe(func([]byte) {}, func([]byte) {}); err == nil {
		t.Fatal("Subscribe must refuse while no characteristics are resolved")
	}
	if err := tr.WriteTX([]byte{0x01}); err == nil {
		t.Fatal("WriteTX must refuse while no TX characteristic is resolved")
	}
}
