//go:build linux

package remap

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func newInjectDaemon() *Daemon {
	return &Daemon{
		mapping:  MergeDefaults(nil),
		matchers: defaultMatchers(),
		clients:  map[*captureClient]struct{}{},
		cfg:      Config{AgentUID: os.Getuid()},
	}
}

// serveInject runs the daemon against a real Unix socket so the connection
// carries kernel credentials (SO_PEERCRED), and returns the client side.
func serveInject(t *testing.T, d *Daemon) net.Conn {
	t.Helper()
	dir, err := os.MkdirTemp("", "h")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	listener, err := net.Listen("unix", filepath.Join(dir, "helper.sock"))
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		d.handleConn(conn)
	}()
	conn, err := net.Dial("unix", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func exchange(t *testing.T, conn net.Conn, w *bufio.Writer, r *bufio.Reader, command map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(command)
	if err != nil {
		t.Fatalf("marshal command: %v", err)
	}
	if _, err := w.Write(append(data, '\n')); err != nil {
		t.Fatalf("write command: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush command: %v", err)
	}
	line, err := r.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	var response map[string]any
	if err := json.Unmarshal(line, &response); err != nil {
		t.Fatalf("decode response %q: %v", line, err)
	}
	return response
}

func TestDaemonInject(t *testing.T) {
	d := newInjectDaemon()
	conn := serveInject(t, d)
	w := bufio.NewWriter(conn)
	r := bufio.NewReader(conn)

	if resp := exchange(t, conn, w, r, map[string]any{"cmd": "inject", "key": "up", "value": 1}); resp["ok"] != true {
		t.Fatalf("allowed key rejected: %+v", resp)
	}
	if resp := exchange(t, conn, w, r, map[string]any{"cmd": "inject", "key": "up", "value": 0}); resp["ok"] != true {
		t.Fatalf("allowed key release rejected: %+v", resp)
	}
	resp := exchange(t, conn, w, r, map[string]any{"cmd": "inject", "key": "ok", "value": 1})
	if resp["ok"] != false || resp["error"] != "key not allowed" {
		t.Fatalf("whitelist miss not rejected: %+v", resp)
	}
	resp = exchange(t, conn, w, r, map[string]any{"cmd": "inject", "key": "down", "value": 7})
	if resp["ok"] != false || resp["error"] != "invalid value" {
		t.Fatalf("invalid value not rejected: %+v", resp)
	}
}

// SEC-01 wiring: a peer without kernel credentials (net.Pipe yields uid -1)
// must not reach privileged commands or even the inject whitelist. Foreign
// uids on a real socket are covered by TestHelperSocketPeerAuth.
func TestDaemonDeniesUnauthenticatedPeer(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	d := newInjectDaemon()
	go d.handleConn(server)
	w := bufio.NewWriter(client)
	r := bufio.NewReader(client)

	for _, cmd := range []map[string]any{
		{"cmd": "inject", "key": "up", "value": 1},
		{"cmd": "apply", "mapping": map[string]string{"up": "up"}},
		{"cmd": "capture_start"},
		{"cmd": "status"},
	} {
		resp := exchange(t, client, w, r, cmd)
		if resp["ok"] != false || resp["error"] != "denied" {
			t.Fatalf("unauthenticated %v not denied: %+v", cmd["cmd"], resp)
		}
	}
}
