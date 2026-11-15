package runtime

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/oekit/logx"

	"github.com/yi-nology/open-voice-bridge/biz/service"
	"github.com/yi-nology/open-voice-bridge/internal/agent"
	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/core"
	"github.com/yi-nology/open-voice-bridge/internal/helper"
	"github.com/yi-nology/open-voice-bridge/internal/history"
	"github.com/yi-nology/open-voice-bridge/internal/input"
)

type depsStubEngine struct{}

func (depsStubEngine) Name() string                { return "stub" }
func (depsStubEngine) Local() bool                 { return true }
func (depsStubEngine) Close(context.Context) error { return nil }
func (depsStubEngine) Transcribe(context.Context, core.ASRAudio) (core.ASRResult, error) {
	return core.ASRResult{Text: "x", Provider: "stub"}, nil
}

// TestAdminDepsLeaveNoFieldUnwired is the mechanical gate for the wiring seam:
// a nil ui.Deps field means its API face answers 503 and the UI control behind
// it is a dead switch (S8 lookback and the history policy both shipped unwired
// once). Every new Deps field must be connected in Admin.deps().
func TestAdminDepsLeaveNoFieldUnwired(t *testing.T) {
	dir := t.TempDir()
	logger := logx.NewSlogLogger("deps-test", logx.WithHandler(slog.NewTextHandler(io.Discard, nil)))
	cfg := config.Config{Address: "AA:BB:CC:DD:EE:FF", MaxUtterance: 30}
	svc := service.New(cfg, filepath.Join(dir, "config.json"),
		agent.New(cfg, depsStubEngine{}, nil), helper.New(filepath.Join(dir, "helper.sock")),
		nil, logger, service.Ops{})
	hub := input.NewHub()
	a := Admin{
		Service:       svc,
		History:       history.New(filepath.Join(dir, "history.jsonl"), 200, history.Policy{}),
		Stats:         history.NewStats(filepath.Join(dir, "usage-stats.json")),
		Hub:           hub,
		Phone:         PhoneAdmin{Enabled: false, Hub: hub, Configured: func() bool { return false }},
		Addr:          "127.0.0.1:8787",
		HelperSocket:  filepath.Join(dir, "helper.sock"),
		Version:       "test",
		MCPBinaryPath: func() string { return "/usr/local/bin/xiaoyao" },
		Logger:        logger,
	}
	v := reflect.ValueOf(a.deps())
	typ := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		switch field.Kind() {
		case reflect.Func, reflect.Interface, reflect.Pointer, reflect.Map, reflect.Slice, reflect.Chan:
			if field.IsNil() {
				t.Errorf("ui.Deps.%s is not wired in Admin.deps() — its API face answers 503", typ.Field(i).Name)
			}
		}
	}
}
