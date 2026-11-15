package runtime

// End-to-end drive of RunAgent (OPS-13): the full construction — config
// load, ASR degrade path, fake sink, fake radio via the ARC-08 transport
// factory seam, admin HTTP — boots to a live /readyz and shuts down cleanly
// through the bootstrap lifecycle, the same path a signal takes.

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oekit/logx"

	"github.com/yi-nology/open-voice-bridge/biz/router"
	"github.com/yi-nology/open-voice-bridge/internal/agent"
	"github.com/yi-nology/open-voice-bridge/internal/atvv"
	"github.com/yi-nology/open-voice-bridge/internal/audio"
	"github.com/yi-nology/open-voice-bridge/internal/config"
)

type e2eSink struct{}

func (e2eSink) Start(context.Context) error { return nil }
func (e2eSink) Write([]int16) error         { return nil }
func (e2eSink) Close(context.Context) error { return nil }
func (e2eSink) Name() string                { return "e2e-sink" }
func (e2eSink) Healthy() bool               { return true }

type e2eTransport struct {
	disconnected chan struct{}
	sent         [][]byte
}

func (t *e2eTransport) Connect(context.Context) error { return nil }
func (t *e2eTransport) Subscribe(audio, control func([]byte)) error {
	return nil
}
func (t *e2eTransport) WriteTX(data []byte) error {
	t.sent = append(t.sent, append([]byte(nil), data...))
	return nil
}
func (t *e2eTransport) Close(context.Context) error {
	select {
	case <-t.disconnected:
	default:
		close(t.disconnected)
	}
	return nil
}
func (t *e2eTransport) Disconnected() <-chan struct{} { return t.disconnected }

func TestRunAgentBootsAndShutsDownCleanly(t *testing.T) {
	dir := t.TempDir()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	cfg := config.Default()
	cfg.Address = "27:17:32:B8:AA:01"
	cfg.UIAddr = addr
	cfg.HelperSocket = filepath.Join(dir, "helper.sock")
	cfg.PasteEnabled = false
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	transport := &e2eTransport{disconnected: make(chan struct{})}
	stop := make(chan struct{})
	codeCh := make(chan int, 1)
	go func() {
		codeCh <- RunAgent(AgentOptions{
			ConfigPath: configPath,
			Version:    "e2e",
			Logger:     logx.NewSlogLogger("agent-e2e"),
			Register:   router.Register,
			NewSink:    func(audio.Config) (audio.Sink, error) { return e2eSink{}, nil },
			TransportFactory: func(string) (agent.Transport, agent.Session, error) {
				return transport, atvv.NewSession(), nil
			},
			Stop: stop,
		})
	}()

	base := "http://" + addr
	deadline := time.Now().Add(20 * time.Second)
	var readyBody []byte
	for {
		resp, err := http.Get(base + "/readyz")
		if err == nil {
			readyBody, _ = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if len(readyBody) > 0 && readyBody[0] == '{' {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("admin HTTP never came up on %s: %v", addr, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	var ready map[string]any
	if err := json.Unmarshal(readyBody, &ready); err != nil {
		t.Fatalf("readyz not JSON: %v\n%s", err, readyBody)
	}
	for _, key := range []string{"ok", "mic_healthy", "asr_ready", "ble_connected"} {
		if _, seen := ready[key]; !seen {
			t.Errorf("readyz missing %q: %s", key, readyBody)
		}
	}
	if ready["mic_healthy"] != true {
		t.Errorf("fake sink reports healthy, readyz says mic_healthy=%v", ready["mic_healthy"])
	}
	if ready["ble_connected"] != true {
		t.Errorf("fake radio connected, readyz says ble_connected=%v", ready["ble_connected"])
	}

	resp, err := http.Get(base + "/api/status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	statusBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	var status map[string]any
	if err := json.Unmarshal(statusBody, &status); err != nil {
		t.Fatalf("status not JSON: %v\n%s", err, statusBody)
	}
	for _, key := range []string{"ble_connected", "mic_active", "asr_backend"} {
		if _, seen := status[key]; !seen {
			t.Errorf("status missing %s: %s", key, statusBody)
		}
	}

	close(stop)
	select {
	case code := <-codeCh:
		if code != 0 {
			t.Fatalf("RunAgent exit code = %d, want 0", code)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("RunAgent did not shut down after Stop closed")
	}
}
