package ui

import (
	"context"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func securityEngine() *server.Hertz {
	s := New(Deps{})
	h := server.New()
	h.Use(s.Security)
	handler := func(ctx context.Context, c *app.RequestContext) {
		c.String(consts.StatusOK, "ok")
	}
	h.GET("/", handler)
	h.POST("/", handler)
	return h
}

func request(h *server.Hertz, method, url string, headers ...ut.Header) *ut.ResponseRecorder {
	return ut.PerformRequest(h.Engine, method, url, &ut.Body{Body: nil, Len: 0}, headers...)
}

func TestSecurityAllowsLoopback(t *testing.T) {
	h := securityEngine()
	resp := request(h, "GET", "http://127.0.0.1:8787/")
	if resp.Code != consts.StatusOK {
		t.Fatalf("GET code=%d body=%s", resp.Code, resp.Body.String())
	}
	resp = request(h, "POST", "http://127.0.0.1:8787/",
		ut.Header{Key: "Origin", Value: "http://127.0.0.1:8787"},
		ut.Header{Key: "Sec-Fetch-Site", Value: "same-origin"},
	)
	if resp.Code != consts.StatusOK {
		t.Fatalf("POST code=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestSecurityRejectsExternalHost(t *testing.T) {
	h := securityEngine()
	resp := request(h, "GET", "http://evil.example/")
	if resp.Code != consts.StatusForbidden {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestSecurityRejectsCrossSitePost(t *testing.T) {
	h := securityEngine()
	resp := request(h, "POST", "http://127.0.0.1:8787/",
		ut.Header{Key: "Origin", Value: "http://evil.example"},
		ut.Header{Key: "Sec-Fetch-Site", Value: "cross-site"},
	)
	if resp.Code != consts.StatusForbidden {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
}
