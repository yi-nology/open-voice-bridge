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

// orderEngine records the name on every call so the chain's attempt order
// is observable.
type orderEngine struct {
	name string
	err  error
	log  *[]string
}

func (o orderEngine) Name() string                    { return o.name }
func (o orderEngine) Local() bool                     { return true }
func (o orderEngine) Close(ctx context.Context) error { return nil }
func (o orderEngine) Transcribe(ctx context.Context, audio Audio) (Result, error) {
	*o.log = append(*o.log, o.name)
	if o.err != nil {
		return Result{}, o.err
	}
	return Result{Text: "好"}, nil
}

// TestChainPrefersTheLastWinner: once the fallback wins, the next call opens
// with it instead of re-probing the broken primary first — one less paid
// failure per utterance and the latency of the winner (health scheduling).
func TestChainPrefersTheLastWinner(t *testing.T) {
	var log []string
	chain := NewChain(
		orderEngine{name: "primary", err: oeerrors.New("boom"), log: &log},
		orderEngine{name: "fallback", log: &log},
	)
	if _, err := chain.Transcribe(context.Background(), Audio{}); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if got := strings.Join(log, ","); got != "primary,fallback" {
		t.Fatalf("first attempt order = %s", got)
	}
	log = log[:0]
	if _, err := chain.Transcribe(context.Background(), Audio{}); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if got := strings.Join(log, ","); got != "fallback" {
		t.Fatalf("second attempt order = %s, want the last winner first", got)
	}
}
