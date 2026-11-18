// Code split from server.go (ARC-09): same package, one concern per file.
package phone

import (
	"context"
	"encoding/json"
	"github.com/coder/websocket"
	"github.com/oekit/concurrency/async"
	"github.com/yi-nology/open-voice-bridge/internal/core"
	"net/http"
	"time"
)

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := s.store.RedeemTicket(r.URL.Query().Get("ticket"))
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "invalid or expired ticket")
		return
	}
	// Accept performs same-origin verification by default; a foreign Origin
	// fails the handshake here, so un-paired cross-site pages cannot inject.
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		log.Warn("phone websocket handshake failed", "device", deviceID)
		return
	}
	name := s.store.DeviceName(deviceID)
	active := s.register(deviceID, conn, name, clientIP(r.RemoteAddr))
	if active == nil {
		_ = conn.Close(websocket.StatusPolicyViolation, "too many clients")
		return
	}
	// DOM-13: whatever ended the session, the socket closes with it.
	defer func() {
		s.unregister(active)
		_ = conn.CloseNow()
	}()

	ctx := r.Context()
	active.send(ctx, readyBody{
		Type:            "ready",
		DeviceID:        deviceID,
		Name:            name,
		MicAvailable:    s.transcriber != nil,
		MaxAudioSeconds: s.maxSamples / core.SampleRate,
		// Asked of the injector rather than copied from ServerConfig: the same
		// object decides whether a press passes, so the page cannot advertise
		// a key the channel would then deny (R5, ARC-12).
		AllowPower: s.injector.Allowed("power"),
	})
	s.serveConn(ctx, conn, active)
}

func (s *Server) serveConn(ctx context.Context, conn *websocket.Conn, active *activeConn) {
	// Server-side heartbeat (CON-07): keeps NATs warm and closes peers that
	// stop ponging even while the read side is idle.
	pingDone := make(chan struct{})
	defer close(pingDone)
	async.GoSafe(func() { s.pingLoop(ctx, conn, pingDone) })

	for {
		// Each read carries an idle deadline (CON-07): a phone that stops
		// heartbeating must not hold its slot forever. Cancellation closes
		// the connection, which ends this loop and unregisters the source.
		readCtx, cancel := context.WithTimeout(ctx, s.idleTimeout)
		typ, data, err := conn.Read(readCtx)
		cancel()
		if err != nil {
			return
		}
		switch typ {
		case websocket.MessageText:
			s.handleControl(ctx, active, data)
		case websocket.MessageBinary:
			s.handleAudio(ctx, active, data)
		}
	}
}

// pingLoop sends protocol-level pings until the session ends. A ping that
// fails to earn a pong closes the connection so the read loop (and the client
// slot) is released.
func (s *Server) pingLoop(ctx context.Context, conn *websocket.Conn, done <-chan struct{}) {
	ticker := time.NewTicker(s.pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, s.pingTimeout)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				_ = conn.CloseNow()
				return
			}
		}
	}
}

type controlMessage struct {
	Type     string `json:"type"`
	Key      string `json:"key,omitempty"`
	Value    *int   `json:"value,omitempty"`
	T        int64  `json:"t,omitempty"`
	Format   string `json:"format,omitempty"`
	Rate     int    `json:"rate,omitempty"`
	Channels int    `json:"channels,omitempty"`
}

func (s *Server) handleControl(ctx context.Context, active *activeConn, data []byte) {
	var msg controlMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		active.send(ctx, errorFrame("bad_message", "malformed json"))
		return
	}
	switch msg.Type {
	case "key":
		s.handleKey(ctx, active, msg)
	case "ping":
		active.send(ctx, pongBody{Type: "pong", T: msg.T})
	case "start":
		s.handleStart(ctx, active, msg)
	case "end":
		s.finishUtterance(ctx, active, false)
	default:
		active.send(ctx, errorFrame("bad_message", "unknown type"))
	}
}

func writeControl(ctx context.Context, conn *websocket.Conn, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		log.Debug("phone websocket write failed", "err", err)
	}
}
