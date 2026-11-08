//go:build linux

package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	oeerrors "github.com/oekit/pkg/errors"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oekit/concurrency/async"
	"github.com/oekit/logx"
)

var log = logx.Delegating("audio")

type pipeWireSink struct {
	cfg       Config
	ctx       context.Context
	mu        sync.Mutex
	player    *exec.Cmd
	stdin     io.WriteCloser
	loop      *exec.Cmd
	closed    bool
	closeOnce sync.Once
	watchOnce sync.Once
	pcm       bytes.Buffer
	healthy   atomic.Bool
}

func newSink(cfg Config) (Sink, error) {
	return &pipeWireSink{cfg: cfg}, nil
}

func (p *pipeWireSink) Name() string { return "PipeWire " + p.cfg.SourceName }

func (p *pipeWireSink) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ctx = ctx
	if err := p.ensureLoopLocked(ctx); err != nil {
		return err
	}
	if err := p.ensurePlayerLocked(ctx); err != nil {
		return err
	}
	p.healthy.Store(nodeExists(ctx, p.cfg.SinkName))
	p.watchOnce.Do(func() { async.GoSafe(func() { p.watch(ctx) }) })
	return nil
}

func (p *pipeWireSink) Healthy() bool { return p.healthy.Load() }

const (
	firstWatchDelay   = 5 * time.Second
	healthyWatchDelay = 15 * time.Second
	maxWatchDelay     = 2 * time.Minute
)

func nextWatchDelay(current time.Duration, healthy bool) time.Duration {
	if healthy {
		return healthyWatchDelay
	}
	if current < healthyWatchDelay {
		return healthyWatchDelay
	}
	next := current * 2
	if next > maxWatchDelay {
		next = maxWatchDelay
	}
	return next
}

func (p *pipeWireSink) watch(ctx context.Context) {
	delay := firstWatchDelay
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = nextWatchDelay(delay, p.check(ctx))
	}
}

func (p *pipeWireSink) check(ctx context.Context) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return true
	}
	if nodeExists(ctx, p.cfg.SinkName) {
		p.healthy.Store(true)
		return true
	}
	p.healthy.Store(false)
	log.Warn("virtual mic node missing; restarting", "sink", p.cfg.SinkName)
	p.dropPlayerLocked()
	if p.loop != nil && p.loop.Process != nil {
		if err := p.loop.Process.Kill(); err != nil {
			log.Debug("kill pw-loopback", "err", err)
		}
		if _, err := p.loop.Process.Wait(); err != nil {
			log.Debug("wait pw-loopback", "err", err)
		}
		p.loop = nil
	}
	ok := true
	if err := p.ensureLoopLocked(ctx); err != nil {
		log.Warn("restart pw-loopback failed", "err", err)
		ok = false
	}
	if err := p.ensurePlayerLocked(ctx); err != nil {
		log.Warn("restart pw-cat failed", "err", err)
		ok = false
	}
	if !ok {
		return false
	}
	up := nodeExists(ctx, p.cfg.SinkName)
	p.healthy.Store(up)
	return up
}

func (p *pipeWireSink) ensureLoopLocked(ctx context.Context) error {
	if p.loop != nil && p.loop.Process != nil {
		return nil
	}
	if nodeExists(ctx, p.cfg.SinkName) {
		return nil
	}
	cmd := exec.CommandContext(
		ctx,
		"pw-loopback", "-n", "xiaomi-remote-mic", "-c", "1", "-m", "[ MONO ]",
		"--capture-props",
		fmt.Sprintf("media.class=Audio/Sink node.name=%s node.description=XiaomiRemoteMicSink", p.cfg.SinkName),
		"--playback-props",
		fmt.Sprintf("media.class=Audio/Source node.name=%s node.description=XiaomiRemoteMic", p.cfg.SourceName),
	)
	if err := cmd.Start(); err != nil {
		return oeerrors.Errorf("start pw-loopback: %w", err)
	}
	p.loop = cmd
	// Wait briefly for the node to appear; a slow PipeWire must not fail startup,
	// because the watchdog will retry with backoff.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if nodeExists(ctx, p.cfg.SinkName) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	log.Debug("virtual mic node not visible yet; watchdog will retry", "sink", p.cfg.SinkName)
	return nil
}

func (p *pipeWireSink) ensurePlayerLocked(ctx context.Context) error {
	if p.player != nil && p.player.Process != nil && p.stdin != nil {
		return nil
	}
	cmd := exec.CommandContext(
		ctx,
		"pw-cat", "--playback", "--target", p.cfg.SinkName,
		"--format", "s16", "--rate", fmt.Sprint(p.cfg.Rate),
		"--channels", "1", "--raw", "-",
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return oeerrors.Errorf("start pw-cat: %w", err)
	}
	p.player = cmd
	p.stdin = stdin
	return nil
}

func (p *pipeWireSink) Write(samples []int16) error {
	if len(samples) == 0 {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	sinkCtx := p.ctx
	if sinkCtx == nil {
		sinkCtx = context.Background()
	}
	if err := p.ensurePlayerLocked(sinkCtx); err != nil {
		return err
	}
	p.pcm.Reset()
	p.pcm.Grow(len(samples) * 2)
	for _, sample := range samples {
		var raw [2]byte
		binary.LittleEndian.PutUint16(raw[:], uint16(sample))
		p.pcm.Write(raw[:])
	}
	if _, err := p.stdin.Write(p.pcm.Bytes()); err != nil {
		// The player died; drop the old process and retry once.
		p.dropPlayerLocked()
		if err := p.ensurePlayerLocked(sinkCtx); err != nil {
			return err
		}
		_, err = p.stdin.Write(p.pcm.Bytes())
		return err
	}
	return nil
}

func (p *pipeWireSink) dropPlayerLocked() {
	if p.stdin != nil {
		if err := p.stdin.Close(); err != nil {
			log.Debug("close pw-cat stdin", "err", err)
		}
	}
	if p.player != nil && p.player.Process != nil {
		if err := p.player.Process.Kill(); err != nil {
			log.Debug("kill pw-cat", "err", err)
		}
		if _, err := p.player.Process.Wait(); err != nil {
			log.Debug("wait pw-cat", "err", err)
		}
	}
	p.player = nil
	p.stdin = nil
}

func (p *pipeWireSink) Close(ctx context.Context) error {
	var closeErr error
	p.closeOnce.Do(func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.closed {
			return
		}
		p.closed = true
		p.healthy.Store(false)
		p.dropPlayerLocked()
		if p.loop != nil && p.loop.Process != nil {
			if err := p.loop.Process.Kill(); err != nil {
				log.Warn("kill pw-loopback", "err", err)
			}
			if _, err := p.loop.Process.Wait(); err != nil {
				log.Warn("wait pw-loopback", "err", err)
			}
			p.loop = nil
		}
	})
	return closeErr
}

func nodeExists(ctx context.Context, name string) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pw-dump")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	var objects []struct {
		Info struct {
			Props map[string]any `json:"props"`
		} `json:"info"`
	}
	if err := json.Unmarshal(out, &objects); err != nil {
		return false
	}
	for _, obj := range objects {
		if value, ok := obj.Info.Props["node.name"].(string); ok && strings.EqualFold(value, name) {
			return true
		}
	}
	return false
}
