//go:build linux

package remap

import (
	"bufio"
	"encoding/json"
	"net"
	"testing"
)

func newInjectDaemon() *Daemon {
	return &Daemon{
		mapping:  MergeDefaults(nil),
		matchers: defaultMatchers(),
		clients:  map[*captureClient]struct{}{},
	}
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
	_ = conn
	return response
}

func TestDaemonInject(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	d := newInjectDaemon()
	go d.handleConn(server)
	w := bufio.NewWriter(client)
	r := bufio.NewReader(client)

	if resp := exchange(t, client, w, r, map[string]any{"cmd": "inject", "key": "up", "value": 1}); resp["ok"] != true {
		t.Fatalf("allowed key rejected: %+v", resp)
	}
	if resp := exchange(t, client, w, r, map[string]any{"cmd": "inject", "key": "up", "value": 0}); resp["ok"] != true {
		t.Fatalf("allowed key release rejected: %+v", resp)
	}
	resp := exchange(t, client, w, r, map[string]any{"cmd": "inject", "key": "ok", "value": 1})
	if resp["ok"] != false || resp["error"] != "key not allowed" {
		t.Fatalf("whitelist miss not rejected: %+v", resp)
	}
	resp = exchange(t, client, w, r, map[string]any{"cmd": "inject", "key": "down", "value": 7})
	if resp["ok"] != false || resp["error"] != "invalid value" {
		t.Fatalf("invalid value not rejected: %+v", resp)
	}
}
