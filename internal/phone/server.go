package phone

import (
	"context"
	"io/fs"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/oekit/concurrency/async"
	"github.com/oekit/logx"

	"github.com/yi-nology/open-voice-bridge/internal/core"
	"github.com/yi-nology/open-voice-bridge/internal/input"
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
	// Assets carries the phone page and its scripts. The composition root
	// supplies the embedded tree so this transport does not import web (R4).
	Assets fs.FS
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
	assets       fs.FS
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
	keyJobs  chan keyJob // ordered injection queue (DOM-07); never closed (#12)
	keyDone  chan struct{}
	keyOnce  sync.Once
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
		maxUtterance = core.DefaultUtteranceSeconds
	}
	s := &Server{
		store:        store,
		injector:     injector,
		hub:          hub,
		cfg:          cfg,
		transcriber:  cfg.Transcriber,
		assets:       cfg.Assets,
		maxSamples:   core.Cap(maxUtterance),
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

func (s *Server) register(deviceID string, ws *websocket.Conn, name, ip string) *activeConn {
	s.mu.Lock()
	var replaced *activeConn
	var samples []int16
	if existing, ok := s.conns[deviceID]; ok {
		_ = existing.ws.CloseNow()
		replaced = existing
		// I5: a reconnecting phone did speak — take the replaced session's
		// buffered samples before the registry forgets it. Only a copy happens
		// under the lock; the transcription runs outside it (R8), exactly like
		// the plain-dropped path in unregister. Without this, a network blip
		// mid-sentence silently ate the half utterance (#10).
		if existing.source.isSpeaking() {
			samples = existing.buffer.take()
		}
		s.dropLocked(existing)
	} else if s.cfg.MaxClients > 0 && len(s.conns) >= s.cfg.MaxClients {
		s.mu.Unlock()
		return nil
	}
	active := &activeConn{
		deviceID: deviceID,
		ws:       ws,
		source:   newSource(deviceID, name, ip),
		buffer:   newUtteranceBuffer(s.maxSamples),
		keyJobs:  make(chan keyJob, 32),
		keyDone:  make(chan struct{}),
	}
	async.GoSafe(func() { s.keyWorker(active) })
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
	s.mu.Unlock()
	if replaced != nil {
		s.transcribeDropped(replaced, samples)
	}
	return active
}

func utteranceMeta(active *activeConn) UtteranceMeta {
	return UtteranceMeta{
		DeviceID:   active.deviceID,
		Name:       active.source.Name(),
		Address:    active.source.Address(),
		SampleRate: core.SampleRate,
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
	active.stopKeyQueue()
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
	_ = active.ws.CloseNow()
	return true
}

// --- JSON helpers ----------------------------------------------------------
