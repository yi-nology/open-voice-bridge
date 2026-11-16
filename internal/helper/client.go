// Package helper talks to the local root remapper over a Unix socket.
package helper

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/oekit/concurrency/async"
	"github.com/oekit/logx"

	"github.com/yi-nology/open-voice-bridge/internal/core"
)

var log = logx.Delegating("helper")

type Client struct {
	Path    string
	Timeout time.Duration
}

func New(path string) *Client {
	if path == "" {
		path = "/run/xiaoyao-helper.sock"
	}
	return &Client{Path: path, Timeout: 10 * time.Second}
}

func (c *Client) dial(ctx context.Context) (net.Conn, error) {
	dialer := net.Dialer{Timeout: c.Timeout}
	return dialer.DialContext(ctx, "unix", c.Path)
}

func (c *Client) Call(ctx context.Context, payload core.HelperRequest) (core.HelperResponse, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return core.HelperResponse{}, err
	}
	defer func() {
		if err := conn.Close(); err != nil {
			log.Debug("close helper connection", "err", err)
		}
	}()
	if err := conn.SetDeadline(time.Now().Add(c.Timeout)); err != nil {
		return core.HelperResponse{}, err
	}
	if err := json.NewEncoder(conn).Encode(payload); err != nil {
		return core.HelperResponse{}, err
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return core.HelperResponse{}, err
	}
	var response core.HelperResponse
	if err := json.Unmarshal(line, &response); err != nil {
		return core.HelperResponse{}, fmt.Errorf("decode helper response: %w", err)
	}
	return response, nil
}

// Inject forwards a whitelisted logical key press/release to the root remapper.
// The helper enforces the whitelist and current keymap profile; this only
// returns an error when the transport fails or the helper rejects the key.
func (c *Client) Inject(ctx context.Context, key string, value int) error {
	response, err := c.Call(ctx, core.HelperRequest{Cmd: "inject", Key: key, Value: value})
	if err != nil {
		return err
	}
	if !response.OK {
		return fmt.Errorf("inject %q rejected: %v", key, response.Error)
	}
	return nil
}

type Capture struct {
	conn   net.Conn
	events chan core.HelperEvent
	done   chan struct{}
	once   sync.Once
}

func (c *Client) StartCapture(ctx context.Context) (*Capture, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	// The handshake (request + first response) is bounded; the deadline is
	// cleared before the event stream starts, which is meant to be long-lived
	// (CON-02).
	if err := conn.SetDeadline(time.Now().Add(c.Timeout)); err != nil {
		_ = conn.Close()
		return nil, err
	}
	reader := bufio.NewReader(conn)
	if err := json.NewEncoder(conn).Encode(core.HelperRequest{Cmd: "capture_start"}); err != nil {
		_ = conn.Close()
		return nil, err
	}
	line, err := reader.ReadBytes('\n')
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	var first core.HelperResponse
	if err := json.Unmarshal(line, &first); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if !first.OK {
		_ = conn.Close()
		return nil, fmt.Errorf("capture start failed: %v", first.Error)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		_ = conn.Close()
		return nil, err
	}
	capture := &Capture{
		conn:   conn,
		events: make(chan core.HelperEvent, 32),
		done:   make(chan struct{}),
	}
	async.GoSafe(func() { capture.readLoop(reader) })
	return capture, nil
}

func (c *Capture) Events() <-chan core.HelperEvent { return c.events }

func (c *Capture) readLoop(reader *bufio.Reader) {
	defer close(c.events)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}
		var event core.HelperEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		select {
		case c.events <- event:
		case <-c.done:
			return
		}
	}
}

func (c *Capture) Stop() error {
	if err := c.conn.SetWriteDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return err
	}
	_, err := c.conn.Write([]byte("{\"cmd\": \"capture_stop\"}\n"))
	return err
}

func (c *Capture) Close(ctx context.Context) error {
	c.once.Do(func() { close(c.done) })
	return c.conn.Close()
}
