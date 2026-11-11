package main

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"

	"github.com/yi-nology/open-voice-bridge/biz/handler/ui"
)

// ARC-06: the non-IDL routes must bind the server handed to register, not a
// package global that a second engine or a later test could overwrite.
func TestRegisterBindsTheUIServerItIsGiven(t *testing.T) {
	srv := ui.New(ui.Deps{
		Inputs: func(context.Context) map[string]any {
			return map[string]any{"enabled": false, "sources": []string{}}
		},
	})
	h := server.New()
	register(h, srv)

	resp := ut.PerformRequest(h.Engine, "GET", "/", nil)
	if resp.Code != 200 || !strings.Contains(resp.Body.String(), "<html") {
		t.Fatalf("root: code=%d body=%.80s", resp.Code, resp.Body.String())
	}
	resp = ut.PerformRequest(h.Engine, "GET", "/api/inputs", nil)
	if resp.Code != 200 || !strings.Contains(resp.Body.String(), `"enabled":false`) {
		t.Fatalf("inputs: code=%d body=%s", resp.Code, resp.Body.String())
	}
	resp = ut.PerformRequest(h.Engine, "GET", "/assets/RC003-remote-photo.png", nil)
	if resp.Code != 200 {
		t.Fatalf("asset: code=%d", resp.Code)
	}
}
