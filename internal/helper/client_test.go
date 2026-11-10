package helper

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func serveUnix(t *testing.T, handle func(net.Conn)) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "helper.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		handle(conn)
	}()
	return path
}

func writeLine(t *testing.T, conn net.Conn, value any) {
	t.Helper()
	if err := json.NewEncoder(conn).Encode(value); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

func TestNewDefaults(t *testing.T) {
	if c := New(""); c.Path != "/run/xiaomi-remote-helper.sock" || c.Timeout != 10*time.Second {
		t.Fatalf("default client = %+v", c)
	}
	if c := New("/custom.sock"); c.Path != "/custom.sock" {
		t.Fatalf("custom path = %q", c.Path)
	}
}

func TestCallSuccess(t *testing.T) {
	path := serveUnix(t, func(conn net.Conn) {
		line, err := bufio.NewReader(conn).ReadBytes('\n')
		if err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		var request map[string]any
		if err := json.Unmarshal(line, &request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if request["cmd"] != "inject" {
			t.Errorf("cmd = %v", request["cmd"])
		}
		writeLine(t, conn, map[string]any{"ok": true, "key": "enter"})
	})
	response, err := New(path).Call(context.Background(), map[string]any{"cmd": "inject", "key": "enter", "value": 1})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if response["ok"] != true || response["key"] != "enter" {
		t.Fatalf("response = %v", response)
	}
}

func TestInjectSuccess(t *testing.T) {
	path := serveUnix(t, func(conn net.Conn) {
		line, err := bufio.NewReader(conn).ReadBytes('\n')
		if err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		var request map[string]any
		if err := json.Unmarshal(line, &request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if request["cmd"] != "inject" || request["key"] != "down" || request["value"] != float64(1) {
			t.Errorf("request = %+v", request)
		}
		writeLine(t, conn, map[string]any{"ok": true, "key": "down", "value": 1})
	})
	if err := New(path).Inject(context.Background(), "down", 1); err != nil {
		t.Fatalf("Inject: %v", err)
	}
}

func TestInjectRejected(t *testing.T) {
	path := serveUnix(t, func(conn net.Conn) {
		if _, err := bufio.NewReader(conn).ReadBytes('\n'); err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		writeLine(t, conn, map[string]any{"ok": false, "error": "key not allowed"})
	})
	if err := New(path).Inject(context.Background(), "ok", 1); err == nil {
		t.Fatal("expected rejection error")
	}
}

func TestCallDialError(t *testing.T) {
	client := New(filepath.Join(t.TempDir(), "missing.sock"))
	client.Timeout = 200 * time.Millisecond
	if _, err := client.Call(context.Background(), map[string]string{"cmd": "ping"}); err == nil {
		t.Fatal("expected dial error")
	}
}

func TestCallDecodeError(t *testing.T) {
	path := serveUnix(t, func(conn net.Conn) {
		if _, err := bufio.NewReader(conn).ReadBytes('\n'); err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		if _, err := conn.Write([]byte("not json\n")); err != nil {
			t.Errorf("write response: %v", err)
		}
	})
	if _, err := New(path).Call(context.Background(), map[string]string{"cmd": "ping"}); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestStartCaptureRejected(t *testing.T) {
	path := serveUnix(t, func(conn net.Conn) {
		if _, err := bufio.NewReader(conn).ReadBytes('\n'); err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		writeLine(t, conn, map[string]any{"ok": false, "error": "denied"})
	})
	if _, err := New(path).StartCapture(context.Background()); err == nil {
		t.Fatal("expected capture rejection")
	}
}

func TestStartCaptureStreamsEvents(t *testing.T) {
	path := serveUnix(t, func(conn net.Conn) {
		reader := bufio.NewReader(conn)
		if _, err := reader.ReadBytes('\n'); err != nil {
			t.Errorf("read start: %v", err)
			return
		}
		writeLine(t, conn, map[string]any{"ok": true})
		writeLine(t, conn, map[string]any{"type": "key", "key": "up"})
		writeLine(t, conn, map[string]any{"type": "key", "key": "enter"})
		for {
			if _, err := reader.ReadBytes('\n'); err != nil {
				return
			}
		}
	})
	capture, err := New(path).StartCapture(context.Background())
	if err != nil {
		t.Fatalf("StartCapture: %v", err)
	}
	defer capture.Close(context.Background())
	for _, want := range []string{"up", "enter"} {
		select {
		case event := <-capture.Events():
			if event["key"] != want {
				t.Fatalf("event = %v, want key %q", event, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for key %q", want)
		}
	}
	if err := capture.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}
