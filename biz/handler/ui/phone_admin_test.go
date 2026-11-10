package ui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func adminEngine(deps Deps) *server.Hertz {
	s := New(deps)
	h := server.New()
	h.Use(s.Security)
	h.GET("/api/inputs", s.Inputs)
	h.POST("/api/phone/pairing", s.PhonePairing)
	h.GET("/api/phone/devices", s.PhoneDevices)
	h.POST("/api/phone/revoke", s.PhoneRevoke)
	return h
}

func TestAdminInputsLoopbackOK(t *testing.T) {
	h := adminEngine(Deps{
		Inputs: func(context.Context) map[string]any {
			return map[string]any{"enabled": true, "sources": []any{map[string]any{"id": "remote"}}}
		},
	})
	resp := ut.PerformRequest(h.Engine, "GET", "http://127.0.0.1:8787/api/inputs", nil)
	if resp.Code != consts.StatusOK {
		t.Fatalf("inputs code = %d body = %s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "remote") {
		t.Fatalf("body = %s", resp.Body.String())
	}
}

func TestAdminPhoneEndpointsUnavailableWhenNil(t *testing.T) {
	h := adminEngine(Deps{})
	cases := []struct{ method, path string }{
		{"GET", "/api/inputs"},
		{"POST", "/api/phone/pairing"},
		{"GET", "/api/phone/devices"},
		{"POST", "/api/phone/revoke"},
	}
	for _, tc := range cases {
		var body *ut.Body
		if tc.method == "POST" {
			body = &ut.Body{Body: bytes.NewBufferString(`{"device_id":"phone-1"}`), Len: 24}
		}
		resp := ut.PerformRequest(h.Engine, tc.method, "http://127.0.0.1:8787"+tc.path, body)
		if resp.Code != consts.StatusServiceUnavailable {
			t.Fatalf("%s %s code = %d, want 503", tc.method, tc.path, resp.Code)
		}
	}
}

func TestAdminRevokeParsesDeviceID(t *testing.T) {
	var got string
	h := adminEngine(Deps{
		PhoneRevoke: func(_ context.Context, deviceID string) map[string]any {
			got = deviceID
			return map[string]any{"ok": true}
		},
	})
	resp := ut.PerformRequest(h.Engine, "POST", "http://127.0.0.1:8787/api/phone/revoke",
		&ut.Body{Body: bytes.NewBufferString(`{"device_id":"phone-42"}`), Len: 24},
		ut.Header{Key: "Origin", Value: "http://127.0.0.1:8787"},
		ut.Header{Key: "Sec-Fetch-Site", Value: "same-origin"},
	)
	if resp.Code != consts.StatusOK {
		t.Fatalf("revoke code = %d body = %s", resp.Code, resp.Body.String())
	}
	if got != "phone-42" {
		t.Fatalf("device id = %q, want phone-42", got)
	}
}

func TestAdminRevokeRejectsMissingID(t *testing.T) {
	h := adminEngine(Deps{
		PhoneRevoke: func(context.Context, string) map[string]any { return map[string]any{"ok": true} },
	})
	resp := ut.PerformRequest(h.Engine, "POST", "http://127.0.0.1:8787/api/phone/revoke",
		&ut.Body{Body: bytes.NewBufferString(`{}`), Len: 2},
		ut.Header{Key: "Origin", Value: "http://127.0.0.1:8787"},
		ut.Header{Key: "Sec-Fetch-Site", Value: "same-origin"},
	)
	if resp.Code != consts.StatusBadRequest {
		t.Fatalf("missing id code = %d, want 400", resp.Code)
	}
}

func TestAdminRejectsCrossSitePairing(t *testing.T) {
	h := adminEngine(Deps{
		PhonePairing: func(context.Context) map[string]any { return map[string]any{"enabled": true} },
	})
	resp := ut.PerformRequest(h.Engine, "POST", "http://127.0.0.1:8787/api/phone/pairing",
		&ut.Body{Body: nil, Len: 0},
		ut.Header{Key: "Origin", Value: "http://evil.example"},
	)
	if resp.Code != consts.StatusForbidden {
		t.Fatalf("cross-site pairing code = %d, want 403", resp.Code)
	}
}
