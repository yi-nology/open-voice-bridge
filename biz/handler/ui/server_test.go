package ui

import (
	"bufio"
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/core"
)

func TestSaveKeymapProfileBackend(t *testing.T) {
	var gotName string
	var gotMapping map[string]string
	server := New(Deps{
		SaveKeymapProfile: func(ctx context.Context, name string, mapping map[string]string) (map[string]any, error) {
			gotName = name
			gotMapping = mapping
			return map[string]any{"ok": true, "config": "{}"}, nil
		},
	})
	resp, err := server.SaveKeymapProfile(context.Background(), "studio", map[string]string{"power": "esc"})
	if err != nil {
		t.Fatalf("SaveKeymapProfile: %v", err)
	}
	if !resp.Ok {
		t.Fatalf("resp = %+v", resp)
	}
	if gotName != "studio" || gotMapping["power"] != "esc" {
		t.Fatalf("unexpected save call: name=%q mapping=%v", gotName, gotMapping)
	}
	if resp.Config == nil || *resp.Config != "{}" {
		t.Fatalf("config = %v", resp.Config)
	}
}

func TestDeleteKeymapProfileBackend(t *testing.T) {
	var gotName string
	server := New(Deps{
		DeleteKeymapProfile: func(ctx context.Context, name string) error {
			gotName = name
			return nil
		},
	})
	resp, err := server.DeleteKeymapProfile(context.Background(), "studio")
	if err != nil {
		t.Fatalf("DeleteKeymapProfile: %v", err)
	}
	if !resp.Ok || gotName != "studio" {
		t.Fatalf("resp=%+v name=%q", resp, gotName)
	}
}

func TestStatusBackendConvertsMapping(t *testing.T) {
	server := New(Deps{
		Status: func(context.Context) core.StatusView {
			return core.StatusView{
				OK:            true,
				Address:       "AA:BB:CC:DD:EE:FF",
				Mapping:       map[string]string{"power": "esc"},
				AsrProviders:  []string{"mimo"},
				KeymapProfile: "coding",
				AsrError:      "missing key",
			}
		},
	})
	resp, err := server.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if resp.Mapping["power"] != "esc" || resp.KeymapProfile != "coding" {
		t.Fatalf("unexpected status: %+v", resp)
	}
	if resp.AsrError == nil || *resp.AsrError != "missing key" {
		t.Fatalf("asr_error = %v", resp.AsrError)
	}
}

// slowCaptureHelper accepts one connection, waits, then plays along with the
// capture handshake so the request is provably inside the helper call while
// the test probes the admin mutex.
func slowCaptureHelper(t *testing.T, delay time.Duration) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "h")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	listener, err := net.Listen("unix", filepath.Join(dir, "helper.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		reader := bufio.NewReader(conn)
		if _, err := reader.ReadBytes('\n'); err != nil {
			return
		}
		time.Sleep(delay)
		_, _ = conn.Write([]byte("{\"ok\":true,\"event\":\"capture_started\"}\n"))
		// Keep the session open until the listener dies.
		for {
			if _, err := reader.ReadBytes('\n'); err != nil {
				return
			}
		}
	}()
	return filepath.Join(dir, "helper.sock")
}

// CON-02: the helper call must run outside s.mu — a wedged helper may not
// freeze Publish (and with it every SSE client and graceful shutdown).
func TestStartCaptureDoesNotBlockPublish(t *testing.T) {
	s := New(Deps{HelperSocket: slowCaptureHelper(t, 600*time.Millisecond)})
	started := make(chan error, 1)
	go func() {
		_, err := s.startCapture(context.Background())
		started <- err
	}()
	time.Sleep(150 * time.Millisecond) // the request is now inside the helper call

	begin := time.Now()
	s.Publish(map[string]any{"event": "probe"})
	if elapsed := time.Since(begin); elapsed > 200*time.Millisecond {
		t.Fatalf("Publish blocked %v behind startCapture: the helper call runs under s.mu", elapsed)
	}

	select {
	case err := <-started:
		if err != nil {
			t.Fatalf("startCapture: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("startCapture never finished")
	}
	if _, err := s.stopCapture(); err != nil {
		t.Fatalf("stopCapture: %v", err)
	}
}
