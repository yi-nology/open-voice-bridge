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
	"github.com/yi-nology/open-voice-bridge/web"
)

var log = logx.Delegating("phone")

// WebSocket liveness (CON-07). The phone page heartbeats every 15 s, so a
// connection silent for longer than the idle timeout is dead or wedged and
// must free its slot; the server ping keeps NAT mappings warm and notices
// peers that stop ponging. The timeouts live on the Server (not package
// globals) so one instance's test tuning can never race another's read loop.
const (
	defaultWSIdleTimeout  = 75 * time.Second
	defaultWSPingInterval = 30 * time.Second
	defaultWSPingTimeout  = 10 * time.Second
)

// ServerConfig tunes the standalone phone listener. It is separate from the
// loopback management UI so a LAN exposure never inherits admin middleware.
type ServerConfig struct {
	Addr                string
	TLSMode             string // "auto" | "off"
	CertDir             string // used only when TLSMode == "auto"
	MaxClients          int
	MaxUtteranceSeconds int // 0 falls back to 30
	// MaxQueuedUtterances bounds the in-flight transcription backlog; 0 falls
	// back to maxQueuedUtterances (SEC-08).
	MaxQueuedUtterances int
	// WSIdleTimeout bounds how long a silent connection may hold its client
	// slot; 0 falls back to defaultWSIdleTimeout. Tests shorten it.
	WSIdleTimeout time.Duration
	Transcriber   Transcriber
}

// Server serves the phone pairing endpoints, the WebSocket key channel and, in
// later phases, the microphone stream. It only accepts paired devices and
// enforces same-origin WebSocket handshakes.
type Server struct {
	store        *Store
	injector     *KeyInjector
	hub          *input.Hub
	certs        *CertificateManager
	cfg          ServerConfig
	transcriber  Transcriber
	maxSamples   int
	queue        *utteranceQueue
	srv          *http.Server
	idleTimeout  time.Duration
	pingInterval time.Duration
	pingTimeout  time.Duration

	mu     sync.Mutex
	conns  map[string]*activeConn
	closed chan struct{}
}

type activeConn struct {
	deviceID string
	ws       *websocket.Conn
	source   *Source
	buffer   *utteranceBuffer
	writeMu  sync.Mutex
}

// send is the only writer on a connection. Transcription finishes on its own
// goroutine, so without this lock a transcript frame and a key_ack could write
// concurrently and the WebSocket library would report a data race.
func (a *activeConn) send(ctx context.Context, payload any) {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	writeControl(ctx, a.ws, payload)
}

func NewServer(store *Store, injector *KeyInjector, hub *input.Hub, cfg ServerConfig) *Server {
	maxUtterance := cfg.MaxUtteranceSeconds
	if maxUtterance <= 0 {
		maxUtterance = 30
	}
	s := &Server{
		store:        store,
		injector:     injector,
		hub:          hub,
		cfg:          cfg,
		transcriber:  cfg.Transcriber,
		maxSamples:   maxUtterance * sampleRate,
		queue:        newUtteranceQueue(cfg.MaxQueuedUtterances),
		idleTimeout:  cfg.WSIdleTimeout,
		pingInterval: defaultWSPingInterval,
		pingTimeout:  defaultWSPingTimeout,
		conns:        map[string]*activeConn{},
		closed:       make(chan struct{}),
	}
	if s.idleTimeout <= 0 {
		s.idleTimeout = defaultWSIdleTimeout
	}
	if cfg.TLSMode == "auto" && cfg.CertDir != "" {
		s.certs = NewCertificateManager(cfg.CertDir)
	}
	if cfg.Transcriber != nil {
		async.GoSafe(s.runTranscriptionQueue)
	}
	s.srv = &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
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
	mux.HandleFunc("/phone/app.js", servePhoneAsset("phone_app.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("/phone/worklet.js", servePhoneAsset("phone_worklet.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("/phone/app.css", servePhoneAsset("phone.css", "text/css; charset=utf-8"))
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
		close(s.closed) // stop the transcription worker
		return s.srv.Shutdown(shutdownCtx)
	case err := <-serveErr:
		close(s.closed)
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

// phoneSecurityHeaders is deliberately NOT the admin UI's policy: that page
// denies the microphone, this one must allow it for getUserMedia. WebSockets
// need ws:/wss: spelled out because CSP treats them as separate schemes from
// the page origin.
func phoneSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; "+
			"connect-src 'self' ws: wss:; media-src 'self'; worker-src 'self'; "+
			"frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	w.Header().Set("Permissions-Policy", "microphone=(self), camera=(), geolocation=(), display-capture=()")
	w.Header().Set("Cache-Control", "no-store")
}

func servePhoneAsset(name, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := web.FS.ReadFile(name)
		if err != nil {
			http.Error(w, "asset not found", http.StatusNotFound)
			return
		}
		phoneSecurityHeaders(w)
		w.Header().Set("Content-Type", contentType)
		w.Write(data)
	}
}

func (s *Server) handlePhonePage(w http.ResponseWriter, r *http.Request) {
	data, err := web.FS.ReadFile("phone.html")
	if err != nil {
		http.Error(w, "phone page not found", http.StatusInternalServerError)
		return
	}
	phoneSecurityHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
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
	// DOM-13: whatever ended the session, the socket closes with it.
	defer func() {
		s.unregister(active)
		conn.CloseNow()
	}()

	ctx := r.Context()
	active.send(ctx, map[string]any{
		"type":              "ready",
		"device_id":         deviceID,
		"name":              name,
		"mic_available":     s.transcriber != nil,
		"max_audio_seconds": s.maxSamples / sampleRate,
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
				conn.CloseNow()
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
		active.send(ctx, map[string]any{"type": "pong", "t": msg.T})
	case "start":
		s.handleStart(ctx, active, msg)
	case "end":
		s.finishUtterance(ctx, active, false)
	default:
		active.send(ctx, errorFrame("bad_message", "unknown type"))
	}
}

func (s *Server) handleKey(ctx context.Context, active *activeConn, msg controlMessage) {
	if msg.Value == nil {
		active.send(ctx, errorFrame("bad_message", "key value required"))
		return
	}
	if err := s.injector.Inject(ctx, msg.Key, *msg.Value); err != nil {
		// The rejected value is never logged, only the reason.
		log.Warn("phone key injection denied", "device", active.deviceID, "key", msg.Key, "err", err)
		active.send(ctx, errorFrame("denied", err.Error()))
		return
	}
	active.source.touch()
	active.send(ctx, map[string]any{"type": "key_ack", "key": msg.Key, "value": *msg.Value})
}

// --- WebSocket microphone -------------------------------------------------

// handleStart opens one hold-to-talk utterance: it validates the format, wins
// the microphone through the hub and clears the buffer before any audio lands.
func (s *Server) handleStart(ctx context.Context, active *activeConn, msg controlMessage) {
	if s.transcriber == nil {
		active.send(ctx, errorFrame("not_ready", "microphone input is not enabled yet"))
		return
	}
	if active.source.isSpeaking() {
		active.send(ctx, errorFrame("bad_state", "an utterance is already in progress"))
		return
	}
	// A full queue means earlier speech is still being transcribed. Refuse
	// here so the user never speaks into an utterance that cannot be queued
	// (SEC-08).
	if s.queue.full() {
		active.send(ctx, errorFrame("busy", "still catching up on earlier speech"))
		return
	}
	if msg.Format != "" && msg.Format != "pcm16" {
		active.send(ctx, errorFrame("bad_format", "only pcm16 is supported"))
		return
	}
	if msg.Rate != 0 && msg.Rate != sampleRate {
		active.send(ctx, errorFrame("bad_format", "audio must be 16 kHz"))
		return
	}
	if msg.Channels != 0 && msg.Channels != 1 {
		active.send(ctx, errorFrame("bad_format", "audio must be mono"))
		return
	}
	// Only one source owns the microphone at a time; the BLE remote may
	// preempt this claim, which is why the buffer is cleared once we own it.
	if s.hub != nil && !s.hub.AcquireAudio(active.deviceID) {
		active.send(ctx, errorFrame("busy", "another source is speaking"))
		return
	}
	active.buffer.reset()
	active.source.setSpeaking(true)
	active.send(ctx, map[string]any{"type": "listening", "active": true})
}

// handleAudio buffers one PCM frame. Frames arriving outside a recording
// session are ignored: a client must not be able to inject audio that was
// never requested.
func (s *Server) handleAudio(ctx context.Context, active *activeConn, data []byte) {
	if !active.source.isSpeaking() {
		return
	}
	ok, full := active.buffer.append(data)
	if !ok {
		active.send(ctx, errorFrame("bad_frame", "malformed pcm frame"))
		return
	}
	if full {
		active.send(ctx, errorFrame("too_long", "utterance truncated at the maximum length"))
		s.finishUtterance(ctx, active, true)
	}
}

// finishUtterance closes the recording, releases the microphone and hands the
// samples to the bounded transcription queue, so a slow provider never stops
// the phone from pressing keys and two sentences can never reach ASR swapped
// (SEC-08).
// preempted handles losing the microphone to a stronger source (DOM-06): the
// phone is told so its UI stops showing "listening", and the words already
// spoken still reach the chain (DOM-04), marked as cut short.
func (s *Server) preempted(active *activeConn, samples []int16) {
	sendCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	active.send(sendCtx, map[string]any{"type": "listening", "active": false})
	active.send(sendCtx, errorFrame("preempted", "another source took the microphone"))
	if len(samples) == 0 || s.transcriber == nil {
		return
	}
	s.queue.offer(utteranceJob{samples: samples, meta: utteranceMeta(active), truncated: true})
}

func (s *Server) finishUtterance(ctx context.Context, active *activeConn, truncated bool) {
	if !active.source.isSpeaking() {
		// Preempted or double "end": the capture is already owned elsewhere.
		// No reset here — the buffer may hold the samples a preemption is
		// about to deliver, and handleStart always starts clean.
		return
	}
	samples := active.buffer.take()
	active.source.setSpeaking(false)
	if s.hub != nil {
		s.hub.ReleaseAudio(active.deviceID)
	}
	active.send(ctx, map[string]any{"type": "listening", "active": false})

	if !s.queue.offer(utteranceJob{
		samples:   samples,
		meta:      utteranceMeta(active),
		truncated: truncated,
		notify:    active,
	}) {
		active.send(ctx, errorFrame("busy", "still catching up on earlier speech"))
	}
}

// runTranscriptionQueue drains finished utterances in capture order. It is the
// only caller of the transcriber for phone audio, which is what makes the
// subtitles and history keep the order the words were spoken in. Jobs run with
// a worker-lifetime context so shutdown can cut an in-flight request (CON-03).
func (s *Server) runTranscriptionQueue() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for {
		select {
		case <-s.closed:
			return
		case job := <-s.queue.ch:
			s.processUtterance(ctx, job)
		}
	}
}

func (s *Server) processUtterance(ctx context.Context, job utteranceJob) {
	meta := job.meta
	meta.Truncated = job.truncated
	result, ok := s.transcriber.TranscribeUtterance(ctx, job.samples, meta)
	if job.notify == nil {
		return
	}
	// A fresh detached context: by the time ASR returns the read loop may
	// already be gone, and a write on a closed connection is a no-op that
	// writeControl logs at debug rather than a panic.
	sendCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !ok {
		job.notify.send(sendCtx, errorFrame("no_speech", "nothing recognised"))
		return
	}
	job.notify.send(sendCtx, map[string]any{
		"type":       "transcript",
		"text":       result.Text,
		"raw_text":   result.RawText,
		"provider":   result.Provider,
		"processed":  result.Processed,
		"latency_ms": result.LatencyMS,
		"at":         result.At.Format(time.RFC3339),
		"truncated":  job.truncated,
	})
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
	active := &activeConn{
		deviceID: deviceID,
		ws:       ws,
		source:   newSource(deviceID, name, ip),
		buffer:   newUtteranceBuffer(s.maxSamples),
	}
	// DOM-06: the hub calls Preempt when the remote steals the microphone.
	// The samples are taken synchronously with the flag flip so a racing
	// "end" cannot wipe them; only the notice goes off-thread.
	active.source.onPreempt = func() {
		samples := active.buffer.take()
		async.GoSafe(func() { s.preempted(active, samples) })
	}
	s.conns[deviceID] = active
	if s.hub != nil {
		s.hub.Register(active.source)
	}
	return active
}

func utteranceMeta(active *activeConn) UtteranceMeta {
	return UtteranceMeta{
		DeviceID:   active.deviceID,
		Name:       active.source.Name(),
		Address:    active.source.Address(),
		SampleRate: sampleRate,
	}
}

func (s *Server) unregister(active *activeConn) {
	s.mu.Lock()
	var samples []int16
	if current, ok := s.conns[active.deviceID]; ok && current == active {
		// A phone killed mid-sentence did speak: take those samples before the
		// registry forgets the session. Only a copy happens under the lock, since
		// the socket is already dying and nothing may block here (R8, I5).
		if active.source.isSpeaking() {
			samples = active.buffer.take()
		}
		s.dropLocked(active)
	}
	s.mu.Unlock()
	active.source.disconnect()
	s.transcribeDropped(active, samples)
}

func (s *Server) dropLocked(active *activeConn) {
	delete(s.conns, active.deviceID)
	if s.hub != nil {
		s.hub.Unregister(active.deviceID)
	}
	active.source.disconnect()
}

// transcribeDropped runs audio left over by a connection that ended while the
// user was holding to talk. Nothing is written back to the phone: the words
// still belong in the focused window, the subtitles and the history (I5), and
// they ride the same ordered queue as live utterances.
func (s *Server) transcribeDropped(active *activeConn, samples []int16) {
	if len(samples) == 0 || s.transcriber == nil {
		return
	}
	s.queue.offer(utteranceJob{samples: samples, meta: utteranceMeta(active)})
}

// Disconnect closes the live session of a device, if it has one. A revoked
// phone must stop sending keys and audio at once rather than at its next
// reconnect (SEC-05); its half-spoken utterance is dropped instead of
// transcribed, because revoking is a deliberate cut.
func (s *Server) Disconnect(deviceID string) bool {
	s.mu.Lock()
	active, ok := s.conns[deviceID]
	if ok {
		s.dropLocked(active)
	}
	s.mu.Unlock()
	if !ok {
		return false
	}
	active.ws.CloseNow()
	return true
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
