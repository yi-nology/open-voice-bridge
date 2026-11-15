// Code split from server.go (ARC-09): same package, one concern per file.
package phone

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"github.com/oekit/concurrency/async"
	"github.com/yi-nology/open-voice-bridge/internal/input"
	"io"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"time"
)

// Handler wires the phone routes; httptest drives this directly with tls=off.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/phone/pair", s.handlePair)
	mux.HandleFunc("/api/phone/ticket", s.handleTicket)
	mux.HandleFunc("/api/phone/ping", s.handlePing)
	mux.HandleFunc("/ws/phone", s.handleWS)
	mux.HandleFunc("/phone", s.handlePhonePage)
	mux.HandleFunc("/phone/app.js", s.servePhoneAsset("phone_app.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("/phone/worklet.js", s.servePhoneAsset("phone_worklet.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("/phone/app.css", s.servePhoneAsset("phone.css", "text/css; charset=utf-8"))
	mux.HandleFunc("/phone/tokens.css", s.servePhoneAsset("tokens.css", "text/css; charset=utf-8"))
	mux.HandleFunc("/manifest.json", s.servePhoneAsset("manifest.json", "application/manifest+json"))
	return s.guardOrigin(mux)
}

// guardOrigin applies the same rule the admin boundary uses. This listener is
// the one exposed to the LAN, and /api/phone/pair spends a pairing code, so a
// foreign page must not reach it. The WebSocket handshake keeps its own check
// inside websocket.Accept.
//
// A fronting proxy (phone.tls: off) must preserve the Host header, or the
// browser's Origin will not match it.
func (s *Server) guardOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !SameOrigin(r.Method, r.Host, r.Header.Get("Origin"), r.Header.Get("Sec-Fetch-Site")) {
			writeJSONError(w, http.StatusForbidden, "cross-origin request")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Listen binds the configured address and resolves the certificate. It is
// separate from Serve so the caller can report a bind failure as a failure
// instead of announcing a listener that never came up (OPS-08).
func (s *Server) Listen() (net.Listener, error) {
	listener, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return nil, err
	}
	if s.cfg.TLSMode == "auto" && s.certs != nil {
		cert, err := s.certs.Get()
		if err != nil {
			_ = listener.Close()
			return nil, err
		}
		s.srv.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
		listener = tls.NewListener(listener, s.srv.TLSConfig)
	}
	return listener, nil
}

// Serve accepts connections on an already-bound listener and shuts down with
// ctx. The listener comes from Listen, which is where a port conflict surfaces.
func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
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

// Addr reports the configured listen address. The bound address, which differs
// when the port is 0, comes from the listener Listen returns.
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
// denies the microphone, this one must allow it for getUserMedia. The shared
// guards and the two policies live in edge/netguard so a fix reaches both;
// the root lends them in through the seams in guard.go (R2).
func phoneSecurityHeaders(w http.ResponseWriter) {
	WriteSecHeaders(w.Header().Set)
	w.Header().Set("Cache-Control", "no-store")
}

func (s *Server) servePhoneAsset(name, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := fs.ReadFile(s.assets, name)
		if err != nil {
			http.Error(w, "asset not found", http.StatusNotFound)
			return
		}
		phoneSecurityHeaders(w)
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(data)
	}
}

func (s *Server) handlePhonePage(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(s.assets, "phone.html")
	if err != nil {
		http.Error(w, "phone page not found", http.StatusInternalServerError)
		return
	}
	phoneSecurityHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

// --- WebSocket key channel -------------------------------------------------

type errorBody struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func errorFrame(code, message string) errorBody {
	return errorBody{Type: "error", Code: code, Message: message}
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
