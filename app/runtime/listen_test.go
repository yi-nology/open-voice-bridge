package runtime

import (
	"bytes"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oekit/logx"

	"github.com/yi-nology/open-voice-bridge/internal/input"
	"github.com/yi-nology/open-voice-bridge/internal/phone"
)

func bufferLogger(buf *bytes.Buffer) logx.Logger {
	return logx.NewSlogLogger("listen-test", logx.WithHandler(slog.NewTextHandler(buf, nil)))
}

// phoneServerOn builds a server that only binds: no injector and no
// transcriber, because these tests never serve a request.
func phoneServerOn(t *testing.T, addr string) *phone.Server {
	t.Helper()
	store, err := phone.NewStore(filepath.Join(t.TempDir(), "phone_tokens.json"), phone.Config{
		PairCodeTTL: time.Minute,
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	return phone.NewServer(store, nil, input.NewHub(), phone.ServerConfig{Addr: addr, TLSMode: "off"})
}

// OPS-08: the announcement used to precede the bind, so an occupied port was
// reported as a running listener and the failure only surfaced later as
// "phone server stopped".
func TestListenPhoneDoesNotAnnounceAnOccupiedPort(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	var buf bytes.Buffer
	listener, ok := listenPhone(bufferLogger(&buf), phoneServerOn(t, occupied.Addr().String()), "off")
	if ok || listener != nil {
		t.Fatalf("listenPhone reported success on an occupied port (ok=%v)", ok)
	}
	logged := buf.String()
	if strings.Contains(logged, "phone input listener started") {
		t.Fatalf("announced a listener that never bound:\n%s", logged)
	}
	if !strings.Contains(logged, "disabling phone input") || !strings.Contains(logged, occupied.Addr().String()) {
		t.Fatalf("the failure must name the address and the consequence:\n%s", logged)
	}
}

// Port 0 is how the tests get a free port, so the announcement has to carry the
// resolved address rather than the configured ":0".
func TestListenPhoneAnnouncesTheBoundAddress(t *testing.T) {
	var buf bytes.Buffer
	listener, ok := listenPhone(bufferLogger(&buf), phoneServerOn(t, "127.0.0.1:0"), "off")
	if !ok {
		t.Fatalf("bind failed:\n%s", buf.String())
	}
	defer listener.Close()
	resolved := listener.Addr().String()
	if strings.HasSuffix(resolved, ":0") {
		t.Fatalf("address was not resolved: %s", resolved)
	}
	logged := buf.String()
	if !strings.Contains(logged, "phone input listener started") || !strings.Contains(logged, resolved) {
		t.Fatalf("the announcement must carry the bound address %s:\n%s", resolved, logged)
	}
}
