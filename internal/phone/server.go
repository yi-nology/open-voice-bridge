package phone

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/oekit/concurrency/async"
	"github.com/oekit/logx"

	"github.com/yi-nology/open-voice-bridge/internal/input"
)

var log = logx.Delegating("phone")

// ServerConfig tunes the standalone phone listener. It is separate from the
// loopback management UI so a LAN exposure never inherits admin middleware.
type ServerConfig struct {
	Addr       string
	TLSMode    string // "auto" | "off"
	CertDir    string // used only when TLSMode == "auto"
	MaxClients int
}

// Server serves the phone pairing endpoints, the WebSocket key channel and, in
// later phases, the microphone stream. It only accepts paired devices and
// enforces same-origin WebSocket handshakes.
type Server struct {
	store    *Store
	injector *KeyInjector
	hub      *input.Hub
	certs    *CertificateManager
	cfg      ServerConfig
	srv      *http.Server

	mu     sync.Mutex
	conns  map[string]*activeConn
	closed chan struct{}
}

type activeConn struct {
	deviceID string
	ws       *websocket.Conn
	source   *Source
}

func NewServer(store *Store, injector *KeyInjector, hub *input.Hub, cfg ServerConfig) *Server {
	s := &Server{
		store:    store,
		injector: injector,
		hub:      hub,
		cfg:      cfg,
		conns:    map[string]*activeConn{},
		closed:   make(chan struct{}),
	}
	if cfg.TLSMode == "auto" && cfg.CertDir != "" {
		s.certs = NewCertificateManager(cfg.CertDir)
	}
	s.srv = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	return s
}

// Handler wires the phone routes; httptest drives this directly with tls=off.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/phone/pair", s.handlePair)
	mux.HandleFunc("/api/phone/ticket", s.handleTicket)
	mux.HandleFunc("/api/phone/ping", s.handlePing)
	mux.HandleFunc("/ws/phone", s.handleWS)
	mux.HandleFunc("/phone", s.handlePhonePage)
	return mux
}

// Serve listens on the configured address and shuts down with ctx.
func (s *Server) Serve(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return err
	}
	if s.cfg.TLSMode == "auto" && s.certs != nil {
		cert, err := s.certs.Get()
		if err != nil {
			return err
		}
		s.srv.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
		listener = tls.NewListener(listener, s.srv.TLSConfig)
	}
	serveErr := make(chan error, 1)
	async.GoSafe(func() {
		if err := s.srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			serveErr <- err
		}
	})
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.srv.Shutdown(shutdownCtx)
	case err := <-serveErr:
		return err
	}
}

// Addr reports the resolved listen address once Serve is bound.
func (s *Server) Addr() string { return s.cfg.Addr }

// ConnectedDevices lists live phone sessions for the dashboard.
func (s *Server) ConnectedDevices() []input.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]input.Status, 0, len(s.conns))
	for _, active := range s.conns {
		out = append(out, active.source.Status())
	}
	return out
}

// --- HTTP handlers ---------------------------------------------------------

type pairRequest struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	var req pairRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Phone"
	}
	deviceID, token, err := s.store.Pair(req.Code, name, clientIP(r.RemoteAddr))
	if err != nil {
		log.Info("phone pairing rejected", "ip", clientIP(r.RemoteAddr))
		writeJSONError(w, http.StatusForbidden, err.Error())
		return
	}
	log.Info("phone paired", "device", deviceID, "name", name)
	writeJSON(w, http.StatusOK, map[string]any{"device_id": deviceID, "token": token})
}

func (s *Server) handleTicket(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := s.store.Authenticate(bearerToken(r))
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	ticket, expires, err := s.store.IssueTicket(deviceID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "ticket unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ticket":     ticket,
		"expires_in": int(time.Until(expires).Seconds()),
	})
}

func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := s.store.Authenticate(bearerToken(r))
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "device_id": deviceID})
}

// handlePhonePage serves the phone PWA shell. The full remote UI lands with
// the desktop wiring task; this stub keeps the route live for Phase 1 tests.
func (s *Server) handlePhonePage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(phonePageStub))
}

// --- WebSocket key channel -------------------------------------------------

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
		conn.Close(websocket.StatusPolicyViolation, "too many clients")
		return
	}
	defer s.unregister(active)

	ctx := r.Context()
	writeControl(ctx, conn, map[string]any{"type": "ready", "device_id": deviceID, "name": name})
	s.serveConn(ctx, conn, active)
}

func (s *Server) serveConn(ctx context.Context, conn *websocket.Conn, active *activeConn) {
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		switch typ {
		case websocket.MessageText:
			s.handleControl(ctx, conn, active, data)
		case websocket.MessageBinary:
			// Microphone audio is Phase 2; Phase 1 declines it explicitly.
			writeControl(ctx, conn, errorFrame("not_ready", "microphone input is not enabled yet"))
		}
	}
}

type controlMessage struct {
	Type  string `json:"type"`
	Key   string `json:"key,omitempty"`
	Value *int   `json:"value,omitempty"`
	T     int64  `json:"t,omitempty"`
}

func (s *Server) handleControl(ctx context.Context, conn *websocket.Conn, active *activeConn, data []byte) {
	var msg controlMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		writeControl(ctx, conn, errorFrame("bad_message", "malformed json"))
		return
	}
	switch msg.Type {
	case "key":
		s.handleKey(ctx, conn, active, msg)
	case "ping":
		writeControl(ctx, conn, map[string]any{"type": "pong", "t": msg.T})
	case "start", "end":
		writeControl(ctx, conn, errorFrame("not_ready", "microphone input is not enabled yet"))
	default:
		writeControl(ctx, conn, errorFrame("bad_message", "unknown type"))
	}
}

func (s *Server) handleKey(ctx context.Context, conn *websocket.Conn, active *activeConn, msg controlMessage) {
	if msg.Value == nil {
		writeControl(ctx, conn, errorFrame("bad_message", "key value required"))
		return
	}
	if err := s.injector.Inject(ctx, msg.Key, *msg.Value); err != nil {
		log.Warn("phone key injection denied", "device", active.deviceID, "key", msg.Key, "err", err)
		writeControl(ctx, conn, errorFrame("denied", err.Error()))
		return
	}
	active.source.touch()
	writeControl(ctx, conn, map[string]any{"type": "key_ack", "key": msg.Key, "value": *msg.Value})
}

// --- connection registry (max clients, duplicate replacement) --------------

func (s *Server) register(deviceID string, ws *websocket.Conn, name, ip string) *activeConn {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.conns[deviceID]; ok {
		existing.ws.CloseNow()
		s.dropLocked(existing)
	} else if s.cfg.MaxClients > 0 && len(s.conns) >= s.cfg.MaxClients {
		return nil
	}
	active := &activeConn{deviceID: deviceID, ws: ws, source: newSource(deviceID, name, ip)}
	s.conns[deviceID] = active
	if s.hub != nil {
		s.hub.Register(active.source)
	}
	return active
}

func (s *Server) unregister(active *activeConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, ok := s.conns[active.deviceID]; ok && current == active {
		s.dropLocked(active)
	}
	active.source.disconnect()
}

func (s *Server) dropLocked(active *activeConn) {
	delete(s.conns, active.deviceID)
	if s.hub != nil {
		s.hub.Unregister(active.deviceID)
	}
	active.source.disconnect()
}

// --- JSON helpers ----------------------------------------------------------

type errorBody struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func errorFrame(code, message string) errorBody {
	return errorBody{Type: "error", Code: code, Message: message}
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

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"ok": false, "error": message})
}

func bearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(auth) > 7 && strings.EqualFold(auth[:7], "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return ""
}

const phonePageStub = `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>Open Voice Bridge</title></head>
<body><p>Phone remote pairing is live. The full remote UI arrives with the desktop input-source wiring.</p></body></html>`
