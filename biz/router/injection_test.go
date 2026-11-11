package router_test

import (
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"

	apihandler "github.com/yi-nology/open-voice-bridge/biz/handler/api"
	"github.com/yi-nology/open-voice-bridge/biz/router"
)

// ARC-06: two engines in one process must keep their own backend. With the old
// package global, constructing the second engine silently rewired the first.
func TestEnginesKeepTheirOwnBackend(t *testing.T) {
	first := &fakeBackend{}
	second := &fakeBackend{}
	h1 := server.New()
	h1.Use(apihandler.WithBackend(first))
	router.GeneratedRegister(h1)
	h2 := server.New()
	h2.Use(apihandler.WithBackend(second))
	router.GeneratedRegister(h2)

	resp := perform(h2, "POST", "/api/apply", `{"mapping":{"f5":"f24"}}`)
	if resp.Code != 200 {
		t.Fatalf("apply on second engine: code=%d body=%s", resp.Code, resp.Body.String())
	}
	if second.applied["f5"] != "f24" {
		t.Fatalf("second backend did not receive the apply: %+v", second.applied)
	}
	if len(first.applied) != 0 {
		t.Fatalf("first backend was rewired by the second engine: %+v", first.applied)
	}
	resp = perform(h1, "POST", "/api/apply", `{"mapping":{"f6":"f23"}}`)
	if resp.Code != 200 {
		t.Fatalf("apply on first engine: code=%d body=%s", resp.Code, resp.Body.String())
	}
	if first.applied["f6"] != "f23" {
		t.Fatalf("first engine was served by another backend: %+v", first.applied)
	}
	if _, leaked := second.applied["f6"]; leaked {
		t.Fatalf("second backend received the first engine's apply: %+v", second.applied)
	}
}

// An engine built without a backend must answer 503 per route, not panic on a
// nil global.
func TestRoutesWithoutBackendAnswerUnavailable(t *testing.T) {
	h := server.New()
	router.GeneratedRegister(h)
	resp := perform(h, "GET", "/healthz", "")
	if resp.Code != 503 {
		t.Fatalf("health without backend: code=%d body=%s", resp.Code, resp.Body.String())
	}
}
