package phone

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestRevokePersistFailureRollsBackStore pins issue #8: a revoke whose disk
// write fails must not leave a half-revoked store. The in-memory tables roll
// back (same contract as Pair), so the operator gets a loud failure while the
// device stays consistently paired — the persisted file can never resurrect a
// "revoked" token on restart, because memory and disk never disagreed.
func TestRevokePersistFailureRollsBackStore(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "phone_tokens.json"), DefaultConfig())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	code, _ := store.GeneratePairCode()
	deviceID, token, err := store.Pair(code, "Pixel", "")
	if err != nil {
		t.Fatalf("pair: %v", err)
	}
	ticketValue, _, err := store.IssueTicket(deviceID)
	if err != nil {
		t.Fatalf("issue ticket: %v", err)
	}

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	revokeErr := store.Revoke(deviceID)
	if revokeErr == nil {
		t.Fatal("revoke must fail loudly when the persist fails")
	}
	if _, ok := store.Authenticate(token); !ok {
		t.Fatal("a failed revoke must leave the device authenticated (rolled back)")
	}
	if _, ok := store.RedeemTicket(ticketValue); !ok {
		t.Fatal("a failed revoke must restore the device's outstanding tickets")
	}
	found := false
	for _, device := range store.ListDevices() {
		if device.ID == deviceID {
			found = true
		}
	}
	if !found {
		t.Fatal("a failed revoke must leave the device listed")
	}

	// Disk and memory agree, so a restart keeps the same answer: no
	// resurrection, no half-state.
	reloaded, err := NewStore(filepath.Join(dir, "phone_tokens.json"), DefaultConfig())
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, ok := reloaded.Authenticate(token); !ok {
		t.Fatal("after a failed revoke the device must still authenticate from disk")
	}

	// With a writable directory the retry revokes for real.
	_ = os.Chmod(dir, 0o700)
	if err := store.Revoke(deviceID); err != nil {
		t.Fatalf("revoke retry: %v", err)
	}
	if _, ok := store.Authenticate(token); ok {
		t.Fatal("a successful revoke must kill the token")
	}
	reloaded, err = NewStore(filepath.Join(dir, "phone_tokens.json"), DefaultConfig())
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, ok := reloaded.Authenticate(token); ok {
		t.Fatal("a successfully revoked token must stay dead after restart")
	}
}

// TestSameDeviceReconnectTranscribesBufferedUtterance pins issue #10 against
// I5: the same device reconnecting (duplicate registration) must not silently
// drop the half utterance buffered by the replaced session — the plain-dropped
// path already transcribes it, and reconnecting is not a deliberate cut.
func TestSameDeviceReconnectTranscribesBufferedUtterance(t *testing.T) {
	transcriber := &fakeTranscriber{ok: true, result: UtteranceResult{Text: "半句话", Provider: "fake"}}
	e := newTestEnv(t, ServerConfig{Transcriber: transcriber, MaxUtteranceSeconds: 30, MaxClients: 4})

	// One device, two sessions: pair once, take two tickets from the same token.
	code, _ := e.store.GeneratePairCode()
	status, paired := postJSON(t, e.http.URL+"/api/phone/pair", pairRequest{Code: code, Name: "Pixel"}, nil)
	if status != http.StatusOK {
		t.Fatalf("pair status = %d body = %v", status, paired)
	}
	token, _ := paired["token"].(string)
	deviceID, _ := paired["device_id"].(string)
	ticketFor := func() string {
		status, resp := postJSON(t, e.http.URL+"/api/phone/ticket", map[string]any{},
			map[string]string{"Authorization": "Bearer " + token})
		if status != http.StatusOK {
			t.Fatalf("ticket status = %d body = %v", status, resp)
		}
		value, _ := resp["ticket"].(string)
		return value
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	conn1, _, err := websocket.Dial(ctx, e.wsURL(ticketFor()), nil)
	if err != nil {
		t.Fatalf("dial 1: %v", err)
	}
	defer conn1.CloseNow()
	if frame := readMessage(t, ctx, conn1); frame["type"] != "ready" {
		t.Fatalf("first frame = %v", frame)
	}
	sendJSON(t, ctx, conn1, map[string]any{"type": "start", "format": "pcm16", "rate": 16000, "channels": 1})
	if frame := readMessage(t, ctx, conn1); frame["type"] != "listening" {
		t.Fatalf("start frame = %v", frame)
	}
	if err := conn1.Write(ctx, websocket.MessageBinary, pcmFrame(320)); err != nil {
		t.Fatalf("binary frame: %v", err)
	}

	// The same device opens a second WebSocket: the register path replaces the
	// first session instead of the network dying first.
	conn2, _, err := websocket.Dial(ctx, e.wsURL(ticketFor()), nil)
	if err != nil {
		t.Fatalf("dial 2: %v", err)
	}
	defer conn2.CloseNow()
	if frame := readMessage(t, ctx, conn2); frame["type"] != "ready" {
		t.Fatalf("second session first frame = %v", frame)
	}

	waitFor(t, func() bool { return len(transcriber.snapshot()) == 1 },
		"the replaced session's buffered samples must reach ASR (I5)")
	call := transcriber.snapshot()[0]
	if call.samples != 320 {
		t.Fatalf("samples = %d, want the 320 buffered by the replaced session", call.samples)
	}
	if call.meta.DeviceID != deviceID {
		t.Fatalf("meta = %+v, want the replaced device labelled", call.meta)
	}
	waitFor(t, func() bool { return len(e.server.ConnectedDevices()) == 1 },
		"the new session must be the one left connected")
}

// TestKeyFrameAfterSessionDropAnswersNotPanics pins issue #12: a key frame
// arriving on a session that the production drop path (reconnect replacement,
// revocation) already removed must get the protocol's busy answer, not a
// "send on closed channel" panic in the read loop.
func TestKeyFrameAfterSessionDropAnswersNotPanics(t *testing.T) {
	e := newTestEnv(t, ServerConfig{})
	_, ticket, deviceID := pairAndTicket(t, e, "Pixel")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, e.wsURL(ticket), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()
	if frame := readMessage(t, ctx, conn); frame["type"] != "ready" {
		t.Fatalf("first frame = %v", frame)
	}

	e.server.mu.Lock()
	var active *activeConn
	for _, candidate := range e.server.conns {
		active = candidate
	}
	e.server.mu.Unlock()
	if active == nil {
		t.Fatal("session not registered")
	}

	if !e.server.Disconnect(deviceID) {
		t.Fatal("Disconnect must report the live session it dropped")
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("handleKey panicked after the session was dropped: %v", r)
		}
	}()
	e.server.handleKey(ctx, active, controlMessage{Type: "key", Key: "up", Value: intPtr(1)})
}

// TestKeyQueueSurvivesTeardownRace stresses the interleaving the panic lived
// in: key sends racing the teardown paths (unregister, Disconnect) under -race.
// The jobs channel is never closed, so no interleaving can panic; sends that
// lose the race fall through to the busy answer.
func TestKeyQueueSurvivesTeardownRace(t *testing.T) {
	e := newTestEnv(t, ServerConfig{})
	_, ticket, deviceID := pairAndTicket(t, e, "Pixel")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, e.wsURL(ticket), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()
	if frame := readMessage(t, ctx, conn); frame["type"] != "ready" {
		t.Fatalf("first frame = %v", frame)
	}
	e.server.mu.Lock()
	var active *activeConn
	for _, candidate := range e.server.conns {
		active = candidate
	}
	e.server.mu.Unlock()

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < 300; i++ {
			e.server.handleKey(ctx, active, controlMessage{Type: "key", Key: "up", Value: intPtr(1)})
		}
	}()
	go func() {
		defer wg.Done()
		e.server.unregister(active)
	}()
	go func() {
		defer wg.Done()
		e.server.Disconnect(deviceID)
	}()
	wg.Wait()
}
