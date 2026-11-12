package phone

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/oekit/concurrency/async"
)

// OPS-08 split the bind out of Serve so the caller can report a port conflict
// before announcing a listener. The pair still has to serve on the listener it
// was handed, and still has to stop when the context is cancelled.
func TestListenAndServeRoundTrip(t *testing.T) {
	env := newTestEnv(t, ServerConfig{Addr: "127.0.0.1:0", TLSMode: "off"})
	listener, err := env.server.Listen()
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	async.GoSafe(func() { done <- env.server.Serve(ctx, listener) })

	resp, err := http.Get("http://" + listener.Addr().String() + "/phone")
	if err != nil {
		t.Fatalf("get the phone page: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return after the context was cancelled")
	}
}

// A second Listen on the same address is the conflict the caller must see, and
// it must come back as an error rather than a listener that cannot accept.
func TestListenReportsAnOccupiedAddress(t *testing.T) {
	env := newTestEnv(t, ServerConfig{Addr: "127.0.0.1:0", TLSMode: "off"})
	first, err := env.server.Listen()
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer first.Close()
	other := newTestEnv(t, ServerConfig{Addr: first.Addr().String(), TLSMode: "off"})
	if listener, err := other.server.Listen(); err == nil {
		_ = listener.Close()
		t.Fatal("Listen accepted an address that is already bound")
	}
}
