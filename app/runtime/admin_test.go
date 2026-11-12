package runtime_test

import (
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/oekit/logx"

	"github.com/yi-nology/open-voice-bridge/app/runtime"
	"github.com/yi-nology/open-voice-bridge/biz/handler/ui"
	"github.com/yi-nology/open-voice-bridge/biz/router"
	"github.com/yi-nology/open-voice-bridge/biz/service"
	"github.com/yi-nology/open-voice-bridge/internal/agent"
	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/helper"
	"github.com/yi-nology/open-voice-bridge/internal/history"
	"github.com/yi-nology/open-voice-bridge/internal/input"
)

func adminLogger() logx.Logger {
	return logx.NewSlogLogger("admin-test", logx.WithHandler(slog.NewTextHandler(io.Discard, nil)))
}

// testAdmin wires real collaborators rather than mocks, so a mis-bound field
// shows up as the wrong payload. Register installs the generated IDL routes
// plus the two custom routes these tests read; the full table is covered by
// the root router test.
func testAdmin(t *testing.T) (runtime.Admin, *history.Store, *history.Stats) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{Address: "AA:BB:CC:DD:EE:FF", MaxUtterance: 30}
	path := filepath.Join(dir, "config.json")
	socket := filepath.Join(dir, "helper.sock")
	svc := service.New(cfg, path, agent.New(cfg, &stubEngine{}, nil), helper.New(socket), nil, adminLogger())
	store := history.New(filepath.Join(dir, "history.jsonl"), 200)
	stats := history.NewStats(filepath.Join(dir, "usage-stats.json"))
	hub := input.NewHub()
	return runtime.Admin{
		Service:       svc,
		History:       store,
		Stats:         stats,
		Hub:           hub,
		Phone:         runtime.PhoneAdmin{Enabled: false, Hub: hub, Configured: func() bool { return false }},
		Addr:          "127.0.0.1:8787",
		HelperSocket:  socket,
		Version:       "test",
		MCPBinaryPath: func() string { return "/usr/local/bin/xiaomi-remote" },
		Logger:        adminLogger(),
		Register: func(h *server.Hertz, u *ui.Server) {
			router.GeneratedRegister(h)
			h.GET("/api/stats", u.Stats)
			h.POST("/api/phone/pairing", u.PhonePairing)
		},
	}, store, stats
}

func adminGet(t *testing.T, h *server.Hertz, path string) *ut.ResponseRecorder {
	t.Helper()
	resp := ut.PerformRequest(h.Engine, "GET", "http://127.0.0.1:8787"+path, nil)
	if resp.Code != consts.StatusOK {
		t.Fatalf("GET %s: code=%d body=%s", path, resp.Code, resp.Body.String())
	}
	return resp
}

// The desktop reads history and usage through the admin edge; both payloads
// must come from the stores this Admin was handed, not from a fresh one.
func TestAdminServesTheStoresItIsGiven(t *testing.T) {
	admin, store, stats := testAdmin(t)
	if err := store.Append(history.Entry{Text: "你好世界", Provider: "mimo", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	stats.Record(2*time.Second, 4)

	h := admin.HTTP(admin.UI())
	if body := adminGet(t, h, "/api/history").Body.String(); !strings.Contains(body, "你好世界") {
		t.Fatalf("history body = %s", body)
	}
	body := adminGet(t, h, "/api/stats").Body.String()
	if !strings.Contains(body, `"utterances":1`) || !strings.Contains(body, `"characters":4`) {
		t.Fatalf("stats body = %s", body)
	}
}

// Deleting a row from the desktop must reach the same store the list reads.
func TestAdminDeleteHistoryReachesTheStore(t *testing.T) {
	admin, store, _ := testAdmin(t)
	if err := store.Append(history.Entry{Text: "第一句"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(history.Entry{Text: "第二句"}); err != nil {
		t.Fatal(err)
	}
	entries := store.List(0)
	h := admin.HTTP(admin.UI())

	payload := `{"id":"` + entries[0].ID + `"}`
	resp := ut.PerformRequest(h.Engine, "POST", "http://127.0.0.1:8787/api/history/delete",
		&ut.Body{Body: strings.NewReader(payload), Len: len(payload)},
		ut.Header{Key: "Content-Type", Value: "application/json"},
		ut.Header{Key: "Sec-Fetch-Site", Value: "same-origin"},
	)
	if resp.Code != consts.StatusOK {
		t.Fatalf("delete: code=%d body=%s", resp.Code, resp.Body.String())
	}
	remaining := store.List(0)
	if len(remaining) != 1 || remaining[0].Text != "第一句" {
		t.Fatalf("store after delete = %+v", remaining)
	}
}

// ARC-06: the routes must bind the server handed to Register, so a second
// engine in the same process cannot serve the first one's payloads.
func TestAdminHTTPBindsTheUIServerItIsGiven(t *testing.T) {
	admin, _, _ := testAdmin(t)
	var bound *ui.Server
	admin.Register = func(h *server.Hertz, u *ui.Server) { bound = u }
	uiServer := admin.UI()
	admin.HTTP(uiServer)
	if bound != uiServer {
		t.Fatalf("Register got %p, want the server UI() returned (%p)", bound, uiServer)
	}
}

// I9: the engine Admin builds must enforce same-origin itself — a caller that
// forgets to add the middleware still gets a loopback-only admin edge.
func TestAdminHTTPKeepsTheEdgeSameOrigin(t *testing.T) {
	admin, _, _ := testAdmin(t)
	h := admin.HTTP(admin.UI())

	resp := ut.PerformRequest(h.Engine, "GET", "http://evil.example/api/history", nil)
	if resp.Code != consts.StatusForbidden {
		t.Fatalf("external host: code=%d body=%s", resp.Code, resp.Body.String())
	}
	resp = ut.PerformRequest(h.Engine, "POST", "http://127.0.0.1:8787/api/history/delete",
		&ut.Body{Body: nil, Len: 0},
		ut.Header{Key: "Origin", Value: "http://evil.example"},
		ut.Header{Key: "Sec-Fetch-Site", Value: "cross-site"},
	)
	if resp.Code != consts.StatusForbidden {
		t.Fatalf("cross-site POST: code=%d body=%s", resp.Code, resp.Body.String())
	}
	resp = ut.PerformRequest(h.Engine, "POST", "http://127.0.0.1:8787/api/phone/pairing", &ut.Body{Body: nil, Len: 0})
	if resp.Code != consts.StatusForbidden {
		t.Fatalf("evidence-free POST: code=%d body=%s", resp.Code, resp.Body.String())
	}
}

// A disabled phone listener still answers the management payloads, so the
// desktop panel renders instead of erroring.
func TestAdminPhonePayloadsSurviveADisabledListener(t *testing.T) {
	admin, _, _ := testAdmin(t)
	h := admin.HTTP(admin.UI())
	resp := ut.PerformRequest(h.Engine, "POST", "http://127.0.0.1:8787/api/phone/pairing",
		&ut.Body{Body: nil, Len: 0},
		ut.Header{Key: "Sec-Fetch-Site", Value: "same-origin"},
	)
	if resp.Code != consts.StatusOK {
		t.Fatalf("pairing: code=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"enabled":false`) {
		t.Fatalf("pairing body = %s", body)
	}
}
