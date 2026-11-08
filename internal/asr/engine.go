package asr

import (
	"context"
	oeerrors "github.com/oekit/pkg/errors"
	"strings"
	"sync"
	"time"

	"github.com/oekit/logx"
)

var log = logx.Delegating("asr")

// ErrNoSpeech means the provider ran correctly but found no speech. The chain
// stops instead of spending money/time on fallback providers.
var ErrNoSpeech = oeerrors.New("no speech detected")

type Audio struct {
	Samples    []int16
	SampleRate int
}

type Result struct {
	Text     string
	Language string
	Provider string
	Latency  time.Duration
}

type Engine interface {
	Name() string
	Local() bool
	Transcribe(ctx context.Context, audio Audio) (Result, error)
	Close(ctx context.Context) error
}

// Chain tries the primary provider and then each fallback in order.
type Chain struct {
	engines []Engine
	mu      sync.RWMutex
	last    string
}

func NewChain(engines ...Engine) *Chain {
	return &Chain{engines: engines}
}

func (c *Chain) Name() string {
	names := make([]string, 0, len(c.engines))
	for _, engine := range c.engines {
		names = append(names, engine.Name())
	}
	return strings.Join(names, " → ")
}

func (c *Chain) Local() bool {
	for _, engine := range c.engines {
		if !engine.Local() {
			return false
		}
	}
	return len(c.engines) > 0
}

func (c *Chain) Transcribe(ctx context.Context, audio Audio) (Result, error) {
	if len(c.engines) == 0 {
		return Result{}, oeerrors.New("no ASR providers configured")
	}
	var failures []error
	for _, engine := range c.engines {
		started := time.Now()
		result, err := engine.Transcribe(ctx, audio)
		if oeerrors.Is(err, ErrNoSpeech) {
			return Result{}, nil
		}
		if err != nil {
			log.Warn("ASR provider failed", "provider", engine.Name(), "err", err)
			failures = append(failures, oeerrors.Errorf("%s: %w", engine.Name(), err))
			continue
		}
		result.Provider = engine.Name()
		result.Latency = time.Since(started)
		if strings.TrimSpace(result.Text) == "" {
			return Result{}, nil
		}
		c.mu.Lock()
		c.last = engine.Name()
		c.mu.Unlock()
		return result, nil
	}
	return Result{}, joinErrors(failures)
}

func (c *Chain) LastProvider() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.last
}

func (c *Chain) Close(ctx context.Context) error {
	var failures []error
	for _, engine := range c.engines {
		if err := engine.Close(ctx); err != nil {
			failures = append(failures, err)
		}
	}
	return joinErrors(failures)
}

// NoSpeech is a helper for provider implementations.
func NoSpeech() error { return ErrNoSpeech }

func IsNoSpeech(err error) bool { return oeerrors.Is(err, ErrNoSpeech) }

func joinErrors(errs []error) error {
	var out error
	for _, err := range errs {
		out = oeerrors.Append(out, err)
	}
	return out
}
