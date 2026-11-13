// Package helper talks to the local root remapper over a Unix socket.
package helper

import (
	"bufio"
	"context"
	"encoding/json"
	oeerrors "github.com/oekit/pkg/errors"
	"net"
	"sync"
	"time"

	"github.com/oekit/concurrency/async"
	"github.com/oekit/logx"
)

var log = logx.Delegating("helper")

type Client struct {
	Path    string
	Timeout time.Duration
}

func New(path string) *Client {
	if path == "" {
		path = "/run/xiaomi-remote-helper.sock"
	}
	return &Client{Path: path, Timeout: 10 * time.Second}
}

func (c *Client) dial(ctx context.Context) (net.Conn, error) {
	dialer := net.Dialer{Timeout: c.Timeout}
	return dialer.DialContext(ctx, "unix", c.Path)
}

func (c *Client) Call(ctx context.Context, payload any) (map[string]any, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := conn.Close(); err != nil {
			log.Debug("close helper connection", "err", err)
		}
	}()
	if err := conn.SetDeadline(time.Now().Add(c.Timeout)); err != nil {
		return nil, err
	}
	if err := json.NewEncoder(conn).Encode(payload); err != nil {
		return nil, err
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	var response map[string]any
	if err := json.Unmarshal(line, &response); err != nil {
		return nil, oeerrors.Errorf("decode helper response: %w", err)
	}
	return response, nil
}

// Inject forwards a whitelisted logical key press/release to the root remapper.
// The helper enforces the whitelist and current keymap profile; this only
// returns an error when the transport fails or the helper rejects the key.
func (c *Client) Inject(ctx context.Context, key string, value int) error {
	response, err := c.Call(ctx, map[string]any{"cmd": "inject", "key": key, "value": value})
	if err != nil {
		return err
	}
	if ok, _ := response["ok"].(bool); !ok {
		return oeerrors.Errorf("inject %q rejected: %v", key, response["error"])
	}
	return nil
}

type Capture struct {
	conn   net.Conn
	events chan map[string]any
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
	if err := json.NewEncoder(conn).Encode(map[string]string{"cmd": "capture_start"}); err != nil {
		_ = conn.Close()
		return nil, err
	}
	line, err := reader.ReadBytes('\n')
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	var first map[string]any
	if err := json.Unmarshal(line, &first); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if ok, _ := first["ok"].(bool); !ok {
		_ = conn.Close()
		return nil, oeerrors.Errorf("capture start failed: %v", first["error"])
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		_ = conn.Close()
		return nil, err
	}
	capture := &Capture{
		conn:   conn,
		events: make(chan map[string]any, 32),
		done:   make(chan struct{}),
	}
	async.GoSafe(func() { capture.readLoop(reader) })
	return capture, nil
}

func (c *Capture) Events() <-chan map[string]any { return c.events }

func (c *Capture) readLoop(reader *bufio.Reader) {
	defer close(c.events)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}
		var event map[string]any
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
