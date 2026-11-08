// Package ui serves the embedded UI, SSE events, and capture state.
package ui

import (
	"context"
	"io/fs"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/oekit/concurrency/async"
	ekithsse "github.com/oekit/hertz/sse"
	"github.com/oekit/logx"

	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/helper"
	"github.com/yi-nology/open-voice-bridge/internal/history"
	"github.com/yi-nology/open-voice-bridge/internal/llm"
	"github.com/yi-nology/open-voice-bridge/internal/text"
	"github.com/yi-nology/open-voice-bridge/web"
)

var log = logx.Delegating("ui")

type Deps struct {
	Status              func(context.Context) map[string]any
	Apply               func(context.Context, map[string]string) (map[string]any, error)
	Reconnect           func(context.Context) error
	RestartMic          func(context.Context) error
	SwitchASR           func(context.Context, string) error
	KeymapProfiles      func(context.Context) (string, map[string]map[string]string)
	ApplyKeymapProfile  func(context.Context, string) (map[string]any, error)
	SaveKeymapProfile   func(context.Context, string, map[string]string) (map[string]any, error)
	DeleteKeymapProfile func(context.Context, string) error
	ASRConfig           func(context.Context) map[string]any
	UpdateASRConfig     func(context.Context, asr.Config) error
	TestASR             func(context.Context, string) []asr.Diagnostic
	LLMConfig           func(context.Context) map[string]any
	UpdateLLMConfig     func(context.Context, llm.Config) error
	TestLLM             func(context.Context, string, bool) []llm.Diagnostic
	SaveLLMKey          func(context.Context, string) error
	History             func(context.Context) []history.Entry
	DeleteHistory       func(context.Context, string) error
	ClearHistory        func(context.Context) error
	TextConfig          func(context.Context) map[string]any
	UpdateTextConfig    func(context.Context, text.Config) error
	Devices             func(context.Context) map[string]any
	DiscoverDevices     func(context.Context) map[string]any
	SelectDevice        func(context.Context, string) error
	SetDeviceEnabled    func(context.Context, string, bool) error
	SaveDevice          func(context.Context, config.DeviceConfig) error
	DeleteDevice        func(context.Context, string) error
	ConnectDevice       func(context.Context, string, bool) error
	Ready               func(context.Context) map[string]any
	HelperSocket        string
}

type sseClient struct {
	ch chan map[string]any
}

type Server struct {
	deps    Deps
	helper  *helper.Client
	mu      sync.Mutex
	clients map[*sseClient]struct{}
	capture *helper.Capture
}

func New(deps Deps) *Server {
	return &Server{
		deps:    deps,
		helper:  helper.New(deps.HelperSocket),
		clients: make(map[*sseClient]struct{}),
	}
}

// AssetsFS exposes the embedded asset tree for the Hertz static handler.
func AssetsFS() fs.FS {
	sub, err := fs.Sub(web.FS, "assets")
	if err != nil {
		panic("ui: embedded web/assets missing: " + err.Error())
	}
	return sub
}

// Security is the loopback/origin middleware applied to every Hertz route.
func (s *Server) Security(ctx context.Context, c *app.RequestContext) {
	c.Response.Header.Set("X-Content-Type-Options", "nosniff")
	c.Response.Header.Set("Referrer-Policy", "no-referrer")
	c.Response.Header.Set("Content-Security-Policy",
		"default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; connect-src 'self'; object-src 'none'; base-uri 'none'")
	c.Response.Header.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	if strings.HasPrefix(string(c.Request.URI().Path()), "/api/") {
		c.Response.Header.Set("Cache-Control", "no-store")
	}
	if !validHost(string(c.Request.Host())) || !validOrigin(c) {
		c.AbortWithStatus(consts.StatusForbidden)
		return
	}
	c.Next(ctx)
}

func validHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	switch host {
	case "127.0.0.1", "localhost", "::1":
		return true
	default:
		return false
	}
}

func validOrigin(c *app.RequestContext) bool {
	if string(c.Method()) != "POST" {
		return true
	}
	site := string(c.Request.Header.Peek("Sec-Fetch-Site"))
	if site != "" && site != "same-origin" && site != "none" {
		return false
	}
	origin := string(c.Request.Header.Peek("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return parsed.Host == string(c.Request.Host())
}

func (s *Server) Root(ctx context.Context, c *app.RequestContext) {
	if string(c.Request.URI().Path()) != "/" {
		c.NotFound()
		return
	}
	data, err := web.FS.ReadFile("ui.html")
	if err != nil {
		c.String(consts.StatusInternalServerError, "ui not found")
		return
	}
	c.Response.Header.Set("Cache-Control", "no-store")
	c.Data(consts.StatusOK, "text/html; charset=utf-8", data)
}

func (s *Server) Events(ctx context.Context, c *app.RequestContext) {
	client := &sseClient{ch: make(chan map[string]any, 16)}
	s.mu.Lock()
	s.clients[client] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.clients, client)
		s.mu.Unlock()
	}()

	stream := ekithsse.NewStream(c)
	defer func() {
		if err := stream.Close(); err != nil {
			log.Debug("close sse stream", "err", err)
		}
	}()
	if err := stream.WriteComment("hello"); err != nil {
		return
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-client.ch:
			if err := stream.Send("", event); err != nil {
				return
			}
		case <-ticker.C:
			if err := stream.KeepAlive(); err != nil {
				return
			}
		}
	}
}

func (s *Server) broadcast(event map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for client := range s.clients {
		select {
		case client.ch <- event:
		default:
			// Slow client: drop this event rather than blocking the agent.
		}
	}
}

func (s *Server) IsCapturing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.capture != nil
}

func (s *Server) relayCapture(capture *helper.Capture) {
	for event := range capture.Events() {
		s.broadcast(event)
	}
	s.broadcast(map[string]any{"event": "capture_closed"})
	s.mu.Lock()
	if s.capture == capture {
		s.capture = nil
	}
	s.mu.Unlock()
	closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := capture.Close(closeCtx); err != nil {
		log.Debug("close capture relay", "err", err)
	}
}

func (s *Server) startCapture(ctx context.Context) (string, error) {
	s.mu.Lock()
	if s.capture != nil {
		s.mu.Unlock()
		return "", errCaptureRunning
	}
	capture, err := s.helper.StartCapture(ctx)
	if err != nil {
		s.mu.Unlock()
		return "", err
	}
	s.capture = capture
	s.mu.Unlock()
	async.GoSafe(func() { s.relayCapture(capture) })
	return "capture_started", nil
}

func (s *Server) stopCapture() (string, error) {
	s.mu.Lock()
	capture := s.capture
	s.mu.Unlock()
	if capture == nil {
		return "not_capturing", nil
	}
	if err := capture.Stop(); err != nil {
		return "", err
	}
	return "stopping", nil
}

// Publish broadcasts a domain event to all connected SSE clients.
func (s *Server) Publish(event map[string]any) { s.broadcast(event) }
