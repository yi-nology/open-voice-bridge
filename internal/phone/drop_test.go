package phone

import (
	"context"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestDroppedConnectionStillTranscribes: losing the socket mid-sentence must
// not lose the words. The capture is drained when the registry forgets the
// session, so the desktop still gets the text (DOM-04/I5).
func TestDroppedConnectionStillTranscribes(t *testing.T) {
	transcriber := &fakeTranscriber{ok: true, result: UtteranceResult{Text: "半句话", Provider: "fake"}}
	e := newTestEnv(t, ServerConfig{Transcriber: transcriber, MaxUtteranceSeconds: 30, MaxClients: 4})
	conn, ctx := dialPhone(t, e, "Pixel")

	sendJSON(t, ctx, conn, map[string]any{"type": "start", "format": "pcm16", "rate": 16000, "channels": 1})
	if frame := readMessage(t, ctx, conn); frame["type"] != "listening" {
		t.Fatalf("start frame = %v", frame)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, pcmFrame(320)); err != nil {
		t.Fatalf("binary frame: %v", err)
	}

	// No "end": the browser dies with the button still held.
	conn.CloseNow()

	waitFor(t, func() bool { return len(transcriber.snapshot()) == 1 }, "a dropped capture must still reach ASR")
	call := transcriber.snapshot()[0]
	if call.samples != 320 {
		t.Fatalf("samples = %d, want the 320 buffered before the drop", call.samples)
	}
	if call.meta.Name != "Pixel" {
		t.Fatalf("meta = %+v, want the dropped device labelled", call.meta)
	}
	waitFor(t, func() bool { return e.hub.AudioOwner() == "" }, "the dropped capture must free the microphone")
	if got := e.server.ConnectedDevices(); len(got) != 0 {
		t.Fatalf("connected = %+v, want the session gone", got)
	}
}

// TestIdleDropTranscribesNothing keeps the drain honest: a phone that disconnects
// between utterances has no buffered words and must not invent a request.
func TestIdleDropTranscribesNothing(t *testing.T) {
	transcriber := &fakeTranscriber{ok: true, result: UtteranceResult{Text: "x", Provider: "fake"}}
	e := newTestEnv(t, ServerConfig{Transcriber: transcriber, MaxUtteranceSeconds: 30, MaxClients: 4})
	conn, ctx := dialPhone(t, e, "Pixel")

	sendJSON(t, ctx, conn, map[string]any{"type": "start", "format": "pcm16", "rate": 16000, "channels": 1})
	readMessage(t, ctx, conn)
	sendJSON(t, ctx, conn, map[string]any{"type": "end"})
	waitFor(t, func() bool { return len(transcriber.snapshot()) == 1 }, "the clean end must transcribe")

	conn.CloseNow()
	waitFor(t, func() bool { return len(e.server.ConnectedDevices()) == 0 }, "the session should be dropped")
	if calls := transcriber.snapshot(); len(calls) != 1 {
		t.Fatalf("transcriptions = %d, want only the one from the clean end", len(calls))
	}
}

// TestDisconnectDropsALiveSession is the revocation path: the admin revoked the
// device, so its open socket must die now, and the half-spoken sentence is
// deliberately discarded rather than pasted.
func TestDisconnectDropsALiveSession(t *testing.T) {
	transcriber := &fakeTranscriber{ok: true, result: UtteranceResult{Text: "不该出现", Provider: "fake"}}
	e := newTestEnv(t, ServerConfig{Transcriber: transcriber, MaxUtteranceSeconds: 30, MaxClients: 4})
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
	sendJSON(t, ctx, conn, map[string]any{"type": "start", "format": "pcm16"})
	if frame := readMessage(t, ctx, conn); frame["type"] != "listening" {
		t.Fatalf("start frame = %v", frame)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, pcmFrame(320)); err != nil {
		t.Fatalf("binary frame: %v", err)
	}
	if owner := e.hub.AudioOwner(); owner != deviceID {
		t.Fatalf("owner = %q, want the phone to hold the mic", owner)
	}

	if !e.server.Disconnect(deviceID) {
		t.Fatal("Disconnect must report a live session it closed")
	}
	if _, _, err := conn.Read(ctx); err == nil {
		t.Fatal("the revoked client must not keep reading frames")
	}
	waitFor(t, func() bool { return e.hub.AudioOwner() == "" }, "a revoked device must free the microphone")
	if got := e.server.ConnectedDevices(); len(got) != 0 {
		t.Fatalf("connected = %+v, want none", got)
	}
	time.Sleep(200 * time.Millisecond)
	if calls := transcriber.snapshot(); len(calls) != 0 {
		t.Fatalf("a revoked session must not deliver text, got %+v", calls)
	}
}

func TestDisconnectUnknownDeviceIsFalse(t *testing.T) {
	e := newTestEnv(t, ServerConfig{MaxClients: 4})
	if e.server.Disconnect("no-such-device") {
		t.Fatal("unknown device must report false")
	}
	if e.hub.AudioOwner() != "" {
		t.Fatalf("owner = %q, want an untouched hub", e.hub.AudioOwner())
	}
}

// TestDisconnectReleasesTheSourceSoPhonesCanReclaim guards the hub side of the
// revocation: the microphone slot must be free for the next device.
func TestDisconnectReleasesTheSourceSoPhonesCanReclaim(t *testing.T) {
	transcriber := &fakeTranscriber{ok: true, result: UtteranceResult{Text: "x", Provider: "fake"}}
	e := newTestEnv(t, ServerConfig{Transcriber: transcriber, MaxUtteranceSeconds: 30, MaxClients: 1})
	conn, ctx := dialPhone(t, e, "Pixel")
	sendJSON(t, ctx, conn, map[string]any{"type": "start", "format": "pcm16"})
	readMessage(t, ctx, conn)

	owner := e.hub.AudioOwner()
	if owner == "" {
		t.Fatal("the phone should own the microphone")
	}
	if !e.server.Disconnect(owner) {
		t.Fatal("Disconnect must close the live session")
	}
	waitFor(t, func() bool { return e.hub.AudioOwner() == "" }, "a revoked device must free the microphone")
	if !e.hub.AcquireAudio("phone-2") {
		t.Fatal("the next source must be able to claim the microphone")
	}
}
