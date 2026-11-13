package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/atvv"
	"github.com/yi-nology/open-voice-bridge/internal/config"
)

// ARC-08: connectLoop must be drivable without a radio. The factory seam and
// the unified backoff are what make the retry contract testable at all.

type fakeTransport struct {
	mu           sync.Mutex
	connectErrs  []error
	calls        int
	subscribeErr error
	writeErr     error
	closed       int
	lost         chan struct{}
}

func (t *fakeTransport) Connect(context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	i := t.calls
	t.calls++
	if i < len(t.connectErrs) {
		return t.connectErrs[i]
	}
	return nil
}

func (t *fakeTransport) Subscribe(func([]byte), func([]byte)) error {
	return t.subscribeErr
}

func (t *fakeTransport) WriteTX([]byte) error { return t.writeErr }

func (t *fakeTransport) Close(context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed++
	return nil
}

func (t *fakeTransport) Disconnected() <-chan struct{} {
	if t.lost == nil {
		t.lost = make(chan struct{})
	}
	return t.lost
}

func (t *fakeTransport) closeCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closed
}

type fakeSession struct{ caps *atvv.Caps }

func (s *fakeSession) Caps() *atvv.Caps            { return s.caps }
func (s *fakeSession) MicOpenCommand() []byte      { return nil }
func (s *fakeSession) HandleControl([]byte) string { return "" }
func (s *fakeSession) HandleAudio([]byte) []int16  { return nil }
func (s *fakeSession) SampleRate() int             { return 16000 }

// TestConnectLoopRetriesUntilConnected drives connectLoop with a factory that
// fails twice before succeeding and asserts the full handshake sequence.
func TestConnectLoopRetriesUntilConnected(t *testing.T) {
	tr := &fakeTransport{connectErrs: []error{errors.New("down"), errors.New("down")}}
	factoryCalls := 0
	a := New(config.Config{Address: "AA:BB:CC:DD:EE:FF"}, nil, nil)
	a.linkStart = time.Millisecond
	a.linkCeiling = 2 * time.Millisecond
	a.SetTransportFactory(func(string) (Transport, Session, error) {
		factoryCalls++
		return tr, &fakeSession{}, nil
	})
	a.ctx, a.cancel = context.WithCancel(context.Background())
	defer a.cancel()

	done := make(chan struct{})
	go func() { a.connectLoop(); close(done) }()

	waitFor(t, func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.connected
	})
	a.cancel()
	<-done

	if factoryCalls != 3 {
		t.Fatalf("factory calls = %d, want 3 (two failures then success)", factoryCalls)
	}
	if tr.closeCount() == 0 {
		t.Fatal("shutdown must close the open transport")
	}
}

// TestConnectLoopClosesTransportOnSubscribeFailure pins that a failed
// subscribe tears the link down before the retry, so no half-open
// connection lingers while the backoff runs.
func TestConnectLoopClosesTransportOnSubscribeFailure(t *testing.T) {
	tr := &fakeTransport{subscribeErr: errors.New("no atvv")}
	a := New(config.Config{Address: "AA:BB:CC:DD:EE:FF"}, nil, nil)
	a.linkStart = time.Millisecond
	a.linkCeiling = 2 * time.Millisecond
	a.SetTransportFactory(func(string) (Transport, Session, error) {
		return tr, &fakeSession{}, nil
	})
	a.ctx, a.cancel = context.WithCancel(context.Background())
	defer a.cancel()

	go a.connectLoop()
	// Each subscribe failure must close the link and start another attempt.
	waitFor(t, func() bool { return tr.calls >= 3 && tr.closeCount() >= 2 })
	a.cancel()
}

// TestBackoffDoublesToCeiling pins the unified pacing: 1,2,4,…capped at the
// ceiling, and reset starts over.
func TestBackoffDoublesToCeiling(t *testing.T) {
	b := linkBackoff{start: time.Millisecond, ceiling: 4 * time.Millisecond}
	b.reset()
	var got []time.Duration
	for i := 0; i < 5; i++ {
		got = append(got, b.current())
		b.step()
	}
	want := []time.Duration{time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond, 4 * time.Millisecond, 4 * time.Millisecond}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("step %d = %v, want %v", i, got[i], want[i])
		}
	}
	b.reset()
	if b.current() != time.Millisecond {
		t.Fatalf("after reset = %v, want %v", b.current(), time.Millisecond)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met within 2s")
}
