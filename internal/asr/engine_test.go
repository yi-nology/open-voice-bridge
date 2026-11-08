package asr

import (
	"context"
	"strings"
	"testing"

	oeerrors "github.com/oekit/pkg/errors"
)

type fakeEngine struct {
	name  string
	text  string
	err   error
	calls *int
}

func (f fakeEngine) Name() string                    { return f.name }
func (f fakeEngine) Local() bool                     { return true }
func (f fakeEngine) Close(ctx context.Context) error { return nil }
func (f fakeEngine) Transcribe(ctx context.Context, audio Audio) (Result, error) {
	if f.calls != nil {
		*f.calls++
	}
	return Result{Text: f.text}, f.err
}

func TestChainFallsBackOnError(t *testing.T) {
	fallbackCalls := 0
	chain := NewChain(
		fakeEngine{name: "primary", err: oeerrors.New("boom")},
		fakeEngine{name: "fallback", text: "你好", calls: &fallbackCalls},
	)
	result, err := chain.Transcribe(context.Background(), Audio{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != "你好" || result.Provider != "fallback" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if fallbackCalls != 1 {
		t.Fatalf("fallback calls = %d", fallbackCalls)
	}
	if chain.LastProvider() != "fallback" {
		t.Fatalf("last provider = %q", chain.LastProvider())
	}
}

func TestChainStopsOnNoSpeech(t *testing.T) {
	fallbackCalls := 0
	chain := NewChain(
		fakeEngine{name: "primary", err: ErrNoSpeech},
		fakeEngine{name: "fallback", text: "should not run", calls: &fallbackCalls},
	)
	result, err := chain.Transcribe(context.Background(), Audio{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != "" {
		t.Fatalf("unexpected text: %q", result.Text)
	}
	if fallbackCalls != 0 {
		t.Fatalf("fallback should not run, calls = %d", fallbackCalls)
	}
}

func TestChainStopsOnEmptyText(t *testing.T) {
	fallbackCalls := 0
	chain := NewChain(
		fakeEngine{name: "primary", text: ""},
		fakeEngine{name: "fallback", text: "should not run", calls: &fallbackCalls},
	)
	result, err := chain.Transcribe(context.Background(), Audio{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != "" {
		t.Fatalf("unexpected text: %q", result.Text)
	}
	if fallbackCalls != 0 {
		t.Fatalf("fallback should not run, calls = %d", fallbackCalls)
	}
}

func TestChainName(t *testing.T) {
	chain := NewChain(fakeEngine{name: "a"}, fakeEngine{name: "b"})
	if chain.Name() != "a → b" {
		t.Fatalf("name = %q", chain.Name())
	}
	if !chain.Local() {
		t.Fatalf("expected local chain")
	}
}

func TestUnavailableEngineStaysUsable(t *testing.T) {
	engine := NewUnavailable(oeerrors.New("bad config"))
	if engine.Name() != "unavailable" {
		t.Fatalf("name = %q", engine.Name())
	}
	if _, err := engine.Transcribe(context.Background(), Audio{}); err == nil || !strings.Contains(err.Error(), "bad config") {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := engine.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
}
