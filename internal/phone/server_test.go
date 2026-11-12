package phone

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/yi-nology/open-voice-bridge/internal/input"
	"github.com/yi-nology/open-voice-bridge/web"
)

type testEnv struct {
	server    *Server
	store     *Store
	transport *fakeTransport
	hub       *input.Hub
	http      *httptest.Server
}

func newTestEnv(t *testing.T, cfg ServerConfig) *testEnv {
	t.Helper()
	if cfg.Assets == nil {
		cfg.Assets = web.FS
	}
	store := newTestStore(t, DefaultConfig())
	transport := &fakeTransport{}
	hub := input.NewHub()
	server := NewServer(store, NewKeyInjector(transport, false), hub, cfg)
	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)
	return &testEnv{server: server, store: store, transport: transport, hub: hub, http: ts}
}

func (e *testEnv) wsURL(ticket string) string {
	return "ws" + strings.TrimPrefix(e.http.URL, "http") + "/ws/phone?ticket=" + ticket
}

func postJSON(t *testing.T, url string, body any, header map[string]string) (int, map[string]any) {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// A browser fetch from the phone page always carries this; tests that mean
	// to look cross-origin override it.
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	for key, value := range header {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// pairAndTicket runs the full pairing handshake and returns bearer + ticket.
func pairAndTicket(t *testing.T, e *testEnv, name string) (token, ticket, deviceID string) {
	t.Helper()
	code, _ := e.store.GeneratePairCode()
	status, paired := postJSON(t, e.http.URL+"/api/phone/pair", pairRequest{Code: code, Name: name}, nil)
	if status != http.StatusOK {
		t.Fatalf("pair status = %d body = %v", status, paired)
	}
	token, _ = paired["token"].(string)
	deviceID, _ = paired["device_id"].(string)
	if token == "" || deviceID == "" {
		t.Fatalf("pair response = %v", paired)
	}

	status, ticketResp := postJSON(t, e.http.URL+"/api/phone/ticket", map[string]any{},
		map[string]string{"Authorization": "Bearer " + token})
	if status != http.StatusOK {
		t.Fatalf("ticket status = %d body = %v", status, ticketResp)
	}
	ticket, _ = ticketResp["ticket"].(string)
	if ticket == "" {
		t.Fatalf("ticket response = %v", ticketResp)
	}
	return token, ticket, deviceID
}

func readMessage(t *testing.T, ctx context.Context, conn *websocket.Conn) map[string]any {
	t.Helper()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("ws read: %v", err)
	}
	var msg map[string]any
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("ws decode: %v (%s)", err, data)
	}
	return msg
}

func TestPhonePairTicketAndKeyRoundTrip(t *testing.T) {
	e := newTestEnv(t, ServerConfig{TLSMode: "off", MaxClients: 4})
	_, ticket, deviceID := pairAndTicket(t, e, "Pixel")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, e.wsURL(ticket), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	ready := readMessage(t, ctx, conn)
	if ready["type"] != "ready" || ready["device_id"] != deviceID || ready["name"] != "Pixel" {
		t.Fatalf("ready = %v", ready)
	}

	press, _ := json.Marshal(controlMessage{Type: "key", Key: "up", Value: intPtr(1)})
	if err := conn.Write(ctx, websocket.MessageText, press); err != nil {
		t.Fatalf("write key: %v", err)
	}
	ack := readMessage(t, ctx, conn)
	if ack["type"] != "key_ack" || ack["key"] != "up" {
		t.Fatalf("ack = %v", ack)
	}
	if len(e.transport.calls) != 1 || e.transport.calls[0] != "up" {
		t.Fatalf("inject calls = %v", e.transport.calls)
	}

	sources := e.hub.Sources()
	if len(sources) != 1 || sources[0].ID != deviceID || sources[0].Kind != input.KindPhone {
		t.Fatalf("hub sources = %+v", sources)
	}
}

func TestPhoneKeyRejectedNotWhitelisted(t *testing.T) {
	e := newTestEnv(t, ServerConfig{TLSMode: "off", MaxClients: 4})
	_, ticket, _ := pairAndTicket(t, e, "Phone")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, e.wsURL(ticket), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()
	readMessage(t, ctx, conn) // ready

	body, _ := json.Marshal(controlMessage{Type: "key", Key: "playpause", Value: intPtr(1)})
	if err := conn.Write(ctx, websocket.MessageText, body); err != nil {
		t.Fatalf("write: %v", err)
	}
	msg := readMessage(t, ctx, conn)
	if msg["type"] != "error" || msg["code"] != "denied" {
		t.Fatalf("expected denied, got %v", msg)
	}
	if len(e.transport.calls) != 0 {
		t.Fatalf("non-whitelisted key reached helper: %v", e.transport.calls)
	}
}

// ARC-12: the page must learn the power-key policy from the greeting, so the
// button can be disabled instead of failing on every press. The flag comes from
// the injector, which is the same object that decides whether a press passes.
func TestPhoneReadyAdvertisesThePowerKeyPolicy(t *testing.T) {
	e := newTestEnv(t, ServerConfig{TLSMode: "off", MaxClients: 4})
	_, ticket, _ := pairAndTicket(t, e, "Phone")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, e.wsURL(ticket), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()
	if ready := readMessage(t, ctx, conn); ready["allow_power"] != false {
		t.Fatalf("ready = %v, want allow_power false by default", ready)
	}

	body, _ := json.Marshal(controlMessage{Type: "key", Key: "power", Value: intPtr(1)})
	if err := conn.Write(ctx, websocket.MessageText, body); err != nil {
		t.Fatalf("write power: %v", err)
	}
	if msg := readMessage(t, ctx, conn); msg["type"] != "error" || msg["code"] != "denied" {
		t.Fatalf("power without the opt-in = %v, want denied", msg)
	}
	if len(e.transport.calls) != 0 {
		t.Fatalf("power reached the helper without the opt-in: %v", e.transport.calls)
	}

	opted := newTestEnv(t, ServerConfig{TLSMode: "off", MaxClients: 4})
	opted.server.injector = NewKeyInjector(opted.transport, true)
	_, optedTicket, _ := pairAndTicket(t, opted, "Phone")
	optedConn, _, err := websocket.Dial(ctx, opted.wsURL(optedTicket), nil)
	if err != nil {
		t.Fatalf("dial opted-in: %v", err)
	}
	defer optedConn.CloseNow()
	if ready := readMessage(t, ctx, optedConn); ready["allow_power"] != true {
		t.Fatalf("ready with the opt-in = %v, want allow_power true", ready)
	}
	if err := optedConn.Write(ctx, websocket.MessageText, body); err != nil {
		t.Fatalf("write opted-in power: %v", err)
	}
	if ack := readMessage(t, ctx, optedConn); ack["type"] != "key_ack" || ack["key"] != "power" {
		t.Fatalf("power with the opt-in = %v, want key_ack", ack)
	}
	if len(opted.transport.calls) != 1 || opted.transport.calls[0] != "power" {
		t.Fatalf("opted-in inject calls = %v", opted.transport.calls)
	}
}

func TestPhonePingRequiresBearer(t *testing.T) {
	e := newTestEnv(t, ServerConfig{TLSMode: "off", MaxClients: 4})
	token, _, _ := pairAndTicket(t, e, "Phone")

	req, _ := http.NewRequest(http.MethodGet, e.http.URL+"/api/phone/ping", nil)
	badResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	badResp.Body.Close()
	if badResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no-auth ping = %d, want 401", badResp.StatusCode)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	okResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ping auth: %v", err)
	}
	okResp.Body.Close()
	if okResp.StatusCode != http.StatusOK {
		t.Fatalf("authed ping = %d, want 200", okResp.StatusCode)
	}
}

func TestTicketReuseRejected(t *testing.T) {
	e := newTestEnv(t, ServerConfig{TLSMode: "off", MaxClients: 4})
	_, ticket, _ := pairAndTicket(t, e, "Phone")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, e.wsURL(ticket), nil)
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	conn.CloseNow()

	// Same ticket is single-use; a second handshake must be refused with 401.
	if _, _, err := websocket.Dial(ctx, e.wsURL(ticket), nil); err == nil {
		t.Fatal("reused ticket should be rejected")
	}
}

func TestUnknownTicketRejected(t *testing.T) {
	e := newTestEnv(t, ServerConfig{TLSMode: "off", MaxClients: 4})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, _, err := websocket.Dial(ctx, e.wsURL("0000000000000000"), nil); err == nil {
		t.Fatal("unknown ticket should be rejected")
	}
}

func TestExpiredTicketRejected(t *testing.T) {
	store, clock := fixedClock(t)
	transport := &fakeTransport{}
	server := NewServer(store, NewKeyInjector(transport, false), input.NewHub(), ServerConfig{TLSMode: "off", MaxClients: 4, Assets: web.FS})
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	code, _ := store.GeneratePairCode()
	_, paired := postJSON(t, ts.URL+"/api/phone/pair", pairRequest{Code: code, Name: "Phone"}, nil)
	token, _ := paired["token"].(string)
	_, ticketResp := postJSON(t, ts.URL+"/api/phone/ticket", map[string]any{},
		map[string]string{"Authorization": "Bearer " + token})
	ticket, _ := ticketResp["ticket"].(string)

	*clock = clock.Add(2 * time.Minute) // past default 60s ticket TTL

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/phone?ticket=" + ticket
	if _, _, err := websocket.Dial(ctx, url, nil); err == nil {
		t.Fatal("expired ticket should be rejected")
	}
}

func TestMaxClientsEnforced(t *testing.T) {
	e := newTestEnv(t, ServerConfig{TLSMode: "off", MaxClients: 1})
	_, ticketA, _ := pairAndTicket(t, e, "PhoneA")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	connA, _, err := websocket.Dial(ctx, e.wsURL(ticketA), nil)
	if err != nil {
		t.Fatalf("dial A: %v", err)
	}
	defer connA.CloseNow()
	readMessage(t, ctx, connA) // ready A accepted

	_, ticketB, _ := pairAndTicket(t, e, "PhoneB")
	connB, _, err := websocket.Dial(ctx, e.wsURL(ticketB), nil)
	if err != nil {
		// Server may reject at handshake or immediately after accept; either
		// way the second client cannot stay connected past the limit.
		return
	}
	defer connB.CloseNow()
	if _, _, err := connB.Read(ctx); err == nil {
		t.Fatal("second client should be closed at the client limit")
	}
}

func TestWSRejectsForeignOrigin(t *testing.T) {
	e := newTestEnv(t, ServerConfig{TLSMode: "off", MaxClients: 4})
	_, ticket, _ := pairAndTicket(t, e, "Phone")

	// A valid ticket proves the request reaches the handshake; the foreign
	// Origin must still be refused by websocket.Accept's same-origin check.
	handshakeURL := e.http.URL + "/ws/phone?ticket=" + ticket
	req, err := http.NewRequest(http.MethodGet, handshakeURL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "SGVsbG8gV29ybGQhIQ==")
	req.Header.Set("Origin", "http://evil.example")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("handshake request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusSwitchingProtocols {
		t.Fatal("foreign origin should not upgrade to websocket")
	}
	if resp.StatusCode < 400 || resp.StatusCode >= 500 {
		t.Fatalf("foreign origin status = %d, want a 4xx client error", resp.StatusCode)
	}
}

func intPtr(value int) *int { return &value }
