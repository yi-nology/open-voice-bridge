package phone

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/yi-nology/open-voice-bridge/internal/input"
)

type fakeTranscriber struct {
	mu     sync.Mutex
	calls  []fakeCall
	result UtteranceResult
	ok     bool
}

type fakeCall struct {
	samples int
	meta    UtteranceMeta
}

func (f *fakeTranscriber) TranscribeUtterance(_ context.Context, samples []int16, meta UtteranceMeta) (UtteranceResult, bool) {
	f.mu.Lock()
	f.calls = append(f.calls, fakeCall{samples: len(samples), meta: meta})
	result, ok := f.result, f.ok
	f.mu.Unlock()
	return result, ok
}

func (f *fakeTranscriber) snapshot() []fakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeCall(nil), f.calls...)
}

func pcmFrame(samples int) []byte {
	frame := make([]byte, samples*2)
	for i := range samples {
		frame[i*2] = byte(i)
		frame[i*2+1] = byte(i >> 8)
	}
	return frame
}

// dialPhone opens a WebSocket through the real pairing + ticket handshake and
// consumes the ready frame.
func dialPhone(t *testing.T, e *testEnv, name string) (*websocket.Conn, context.Context) {
	t.Helper()
	_, ticket, _ := pairAndTicket(t, e, name)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, e.wsURL(ticket), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	if frame := readMessage(t, ctx, conn); frame["type"] != "ready" {
		t.Fatalf("first frame = %v, want ready", frame)
	}
	return conn, ctx
}

func sendJSON(t *testing.T, ctx context.Context, conn *websocket.Conn, payload map[string]any) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// expectQuiet proves the server stayed silent: frames on one connection are
// ordered, so a ping/pong round-trip whose first reply is the pong means no
// other frame was emitted in between. A timed-out read cannot be used here —
// coder/websocket closes the connection when a read context expires.
func expectQuiet(t *testing.T, ctx context.Context, conn *websocket.Conn) {
	t.Helper()
	sendJSON(t, ctx, conn, map[string]any{"type": "ping", "t": 1})
	for {
		frame := readMessage(t, ctx, conn)
		if frame["type"] == "pong" {
			return
		}
		t.Fatalf("expected silence, got %v", frame)
	}
}

func waitFor(t *testing.T, cond func() bool, format string, args ...any) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out: "+format, args...)
}

func TestPhoneMicrophoneRoundTripThroughSharedChain(t *testing.T) {
	transcriber := &fakeTranscriber{
		ok:     true,
		result: UtteranceResult{Text: "你好世界", RawText: "你好 world", Provider: "fake", Processed: true, LatencyMS: 1300, At: time.Now()},
	}
	e := newTestEnv(t, ServerConfig{Transcriber: transcriber, MaxUtteranceSeconds: 30, MaxClients: 4})
	conn, ctx := dialPhone(t, e, "Pixel")

	sendJSON(t, ctx, conn, map[string]any{"type": "start", "format": "pcm16", "rate": 16000, "channels": 1})
	if frame := readMessage(t, ctx, conn); frame["type"] != "listening" || frame["active"] != true {
		t.Fatalf("start should open the mic, got %v", frame)
	}
	if owner := e.hub.AudioOwner(); owner == "" {
		t.Fatal("the phone should own the microphone while recording")
	}
	for i := 0; i < 2; i++ {
		if err := conn.Write(ctx, websocket.MessageBinary, pcmFrame(320)); err != nil {
			t.Fatalf("binary frame %d: %v", i, err)
		}
	}
	sendJSON(t, ctx, conn, map[string]any{"type": "end"})
	if frame := readMessage(t, ctx, conn); frame["type"] != "listening" || frame["active"] != false {
		t.Fatalf("end should close the mic, got %v", frame)
	}

	var transcript map[string]any
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline) && transcript == nil; {
		if frame := readMessage(t, ctx, conn); frame["type"] == "transcript" {
			transcript = frame
		}
	}
	if transcript == nil {
		t.Fatal("no transcript frame arrived")
	}
	if transcript["text"] != "你好世界" || transcript["processed"] != true {
		t.Fatalf("transcript frame = %v", transcript)
	}
	if transcript["latency_ms"] != float64(1300) {
		t.Fatalf("latency = %v, want 1300", transcript["latency_ms"])
	}
	at, isString := transcript["at"].(string)
	if !isString || !strings.Contains(at, "T") {
		t.Fatalf("transcript at = %v, want an RFC3339 timestamp", transcript["at"])
	}

	calls := transcriber.snapshot()
	if len(calls) != 1 {
		t.Fatalf("transcribe calls = %d, want 1", len(calls))
	}
	if calls[0].samples != 640 {
		t.Fatalf("samples = %d, want 640 (two 20 ms frames)", calls[0].samples)
	}
	if calls[0].meta.Name != "Pixel" || calls[0].meta.DeviceID == "" {
		t.Fatalf("meta = %+v, want the paired device labelled Pixel", calls[0].meta)
	}
	waitFor(t, func() bool { return e.hub.AudioOwner() == "" }, "microphone still owned after the utterance")

	status := e.server.ConnectedDevices()
	if len(status) != 1 || status[0].Speaking {
		t.Fatalf("device should be connected and idle, got %+v", status)
	}
}

func TestPhoneReadyAdvertisesMicrophone(t *testing.T) {
	e := newTestEnv(t, ServerConfig{Transcriber: &fakeTranscriber{ok: true}, MaxUtteranceSeconds: 12})
	_, ticket, _ := pairAndTicket(t, e, "Pixel")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, e.wsURL(ticket), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()
	frame := readMessage(t, ctx, conn)
	if frame["mic_available"] != true {
		t.Fatalf("ready = %v, want mic_available true", frame)
	}
	if frame["max_audio_seconds"] != float64(12) {
		t.Fatalf("ready = %v, want max_audio_seconds 12", frame)
	}

	// A server without a transcriber must advertise the microphone as absent
	// so the page can hide hold-to-talk instead of failing on every press.
	quiet := newTestEnv(t, ServerConfig{MaxUtteranceSeconds: 30})
	_, quietTicket, _ := pairAndTicket(t, quiet, "Pixel")
	quietConn, _, err := websocket.Dial(ctx, quiet.wsURL(quietTicket), nil)
	if err != nil {
		t.Fatalf("dial quiet: %v", err)
	}
	defer quietConn.CloseNow()
	if frame := readMessage(t, ctx, quietConn); frame["mic_available"] != false {
		t.Fatalf("ready without a transcriber = %v, want mic_available false", frame)
	}
}

func TestPhoneMicrophoneBusyWhenAnotherSourceHoldsIt(t *testing.T) {
	e := newTestEnv(t, ServerConfig{Transcriber: &fakeTranscriber{ok: true}, MaxUtteranceSeconds: 30})
	conn, ctx := dialPhone(t, e, "Pixel")
	e.hub.AcquireAudio(input.RemoteID)

	sendJSON(t, ctx, conn, map[string]any{"type": "start"})
	frame := readMessage(t, ctx, conn)
	if frame["type"] != "error" || frame["code"] != "busy" {
		t.Fatalf("start while busy = %v, want error/busy", frame)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, pcmFrame(320)); err != nil {
		t.Fatalf("binary frame: %v", err)
	}
	sendJSON(t, ctx, conn, map[string]any{"type": "end"})
	expectQuiet(t, ctx, conn)
	if got := len(e.server.ConnectedDevices()); got != 1 {
		t.Fatalf("connected devices = %d", got)
	}
	if owner := e.hub.AudioOwner(); owner != input.RemoteID {
		t.Fatalf("owner = %q, a rejected phone start must not release the remote's claim", owner)
	}
}

func TestPhoneMicrophoneRejectsBadFormatAndStrayAudio(t *testing.T) {
	transcriber := &fakeTranscriber{ok: true}
	e := newTestEnv(t, ServerConfig{Transcriber: transcriber, MaxUtteranceSeconds: 30})
	conn, ctx := dialPhone(t, e, "Pixel")

	sendJSON(t, ctx, conn, map[string]any{"type": "start", "format": "opus"})
	if frame := readMessage(t, ctx, conn); frame["code"] != "bad_format" {
		t.Fatalf("opus start = %v, want bad_format", frame)
	}
	sendJSON(t, ctx, conn, map[string]any{"type": "start", "rate": 48000})
	if frame := readMessage(t, ctx, conn); frame["code"] != "bad_format" {
		t.Fatalf("48 kHz start = %v, want bad_format", frame)
	}
	sendJSON(t, ctx, conn, map[string]any{"type": "start", "channels": 2})
	if frame := readMessage(t, ctx, conn); frame["code"] != "bad_format" {
		t.Fatalf("stereo start = %v, want bad_format", frame)
	}
	if owner := e.hub.AudioOwner(); owner != "" {
		t.Fatalf("owner = %q, a rejected start must not claim the microphone", owner)
	}

	// Audio outside a recording session is ignored, never transcribed.
	if err := conn.Write(ctx, websocket.MessageBinary, pcmFrame(320)); err != nil {
		t.Fatalf("binary frame: %v", err)
	}
	sendJSON(t, ctx, conn, map[string]any{"type": "end"})
	expectQuiet(t, ctx, conn)
	if got := len(transcriber.snapshot()); got != 0 {
		t.Fatalf("transcribe calls = %d, want 0", got)
	}
}

func TestPhoneMicrophoneTruncatesAtMaxDuration(t *testing.T) {
	transcriber := &fakeTranscriber{ok: true, result: UtteranceResult{Text: "长句"}}
	e := newTestEnv(t, ServerConfig{Transcriber: transcriber, MaxUtteranceSeconds: 1})
	conn, ctx := dialPhone(t, e, "Pixel")

	sendJSON(t, ctx, conn, map[string]any{"type": "start"})
	readMessage(t, ctx, conn)
	// 1 s cap = 16000 samples; 320-sample frames cross it at frame 50, so the
	// 60 frames sent here must stop being buffered after the cap.
	for i := 0; i < 60; i++ {
		if err := conn.Write(ctx, websocket.MessageBinary, pcmFrame(320)); err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
	}

	seen := map[string]bool{}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !(seen["too_long"] && seen["transcript"] && seen["idle"]) {
		frame := readMessage(t, ctx, conn)
		switch frame["type"] {
		case "error":
			if frame["code"] == "too_long" {
				seen["too_long"] = true
			}
		case "listening":
			if frame["active"] == false {
				seen["idle"] = true
			}
		case "transcript":
			seen["transcript"] = true
		}
	}
	if !seen["too_long"] {
		t.Fatalf("expected a too_long notice when the cap is hit, saw %v", seen)
	}
	if !seen["transcript"] {
		t.Fatalf("truncated audio must still be transcribed, saw %v", seen)
	}
	if !seen["idle"] {
		t.Fatalf("truncation must also report the mic closing, saw %v", seen)
	}
	calls := transcriber.snapshot()
	if len(calls) != 1 {
		t.Fatalf("transcribe calls = %d, want 1", len(calls))
	}
	if calls[0].samples != 16000 {
		t.Fatalf("samples = %d, want exactly the 1 s cap", calls[0].samples)
	}
}

func TestPhoneNoSpeechReportsNoSpeech(t *testing.T) {
	e := newTestEnv(t, ServerConfig{Transcriber: &fakeTranscriber{ok: false}, MaxUtteranceSeconds: 30})
	conn, ctx := dialPhone(t, e, "Pixel")
	sendJSON(t, ctx, conn, map[string]any{"type": "start"})
	readMessage(t, ctx, conn)
	if err := conn.Write(ctx, websocket.MessageBinary, pcmFrame(320)); err != nil {
		t.Fatalf("binary frame: %v", err)
	}
	sendJSON(t, ctx, conn, map[string]any{"type": "end"})
	readMessage(t, ctx, conn) // listening false
	frame := readMessage(t, ctx, conn)
	if frame["type"] != "error" || frame["code"] != "no_speech" {
		t.Fatalf("empty utterance = %v, want error/no_speech", frame)
	}
}

func TestPhoneSecondStartIsRejectedNotSilentlyRestarted(t *testing.T) {
	e := newTestEnv(t, ServerConfig{Transcriber: &fakeTranscriber{ok: true}, MaxUtteranceSeconds: 30})
	conn, ctx := dialPhone(t, e, "Pixel")
	sendJSON(t, ctx, conn, map[string]any{"type": "start"})
	readMessage(t, ctx, conn)
	sendJSON(t, ctx, conn, map[string]any{"type": "start"})
	if frame := readMessage(t, ctx, conn); frame["code"] != "bad_state" {
		t.Fatalf("second start = %v, want error/bad_state", frame)
	}
	// The in-flight utterance survives the rejected start.
	if err := conn.Write(ctx, websocket.MessageBinary, pcmFrame(320)); err != nil {
		t.Fatalf("binary frame: %v", err)
	}
	sendJSON(t, ctx, conn, map[string]any{"type": "end"})
	if frame := readMessage(t, ctx, conn); frame["active"] != false {
		t.Fatalf("end = %v, want the mic closing", frame)
	}
}

// CON-07: a phone that stops talking AND stops heartbeating must not hold a
// client slot forever. The read loop carries an idle deadline.
func TestIdlePhoneConnectionExpires(t *testing.T) {
	e := newTestEnv(t, ServerConfig{
		Transcriber:         &fakeTranscriber{ok: true},
		MaxUtteranceSeconds: 30,
		MaxClients:          4,
		WSIdleTimeout:       300 * time.Millisecond,
	})
	conn, _ := dialPhone(t, e, "Pixel")
	_ = conn // stay connected but silent: no control messages at all

	waitFor(t, func() bool { return len(e.server.ConnectedDevices()) == 0 },
		"idle phone connection should expire and free its slot")
}

// DOM-06 / I1: when the BLE remote force-claims the microphone, the phone
// must be told, must stop capturing at once, and the words already spoken
// still reach the shared chain (DOM-04).
func TestRemotePreemptionStopsPhoneCapture(t *testing.T) {
	transcriber := &fakeTranscriber{ok: true, result: UtteranceResult{Text: "半句", At: time.Now()}}
	e := newTestEnv(t, ServerConfig{Transcriber: transcriber, MaxUtteranceSeconds: 30, MaxClients: 4})
	conn, ctx := dialPhone(t, e, "Pixel")

	sendJSON(t, ctx, conn, map[string]any{"type": "start", "format": "pcm16", "rate": 16000, "channels": 1})
	if frame := readMessage(t, ctx, conn); frame["type"] != "listening" || frame["active"] != true {
		t.Fatalf("start = %v", frame)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, pcmFrame(320)); err != nil {
		t.Fatal(err)
	}
	// Frames on one connection are processed in order: once the ping is
	// answered, the audio frame is in the buffer and preemption cannot race
	// ahead of it.
	expectQuiet(t, ctx, conn)

	// The remote's claim wins — exactly how the agent claims for RC003 audio.
	e.hub.AcquireAudioForce(input.RemoteID)

	sawListening, sawPreempted := false, false
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline) && !(sawListening && sawPreempted); {
		frame := readMessage(t, ctx, conn)
		if frame["type"] == "listening" && frame["active"] == false {
			sawListening = true
		}
		if frame["type"] == "error" && frame["code"] == "preempted" {
			sawPreempted = true
		}
	}
	if !sawListening || !sawPreempted {
		t.Fatalf("preemption notice missing: listening=%v preempted=%v", sawListening, sawPreempted)
	}

	// Capture stopped: this stray frame must not join any later utterance.
	if err := conn.Write(ctx, websocket.MessageBinary, pcmFrame(320)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(transcriber.snapshot()) == 1 }, "partial utterance should reach the chain")
	if got := transcriber.snapshot()[0].samples; got != 320 {
		t.Fatalf("partial samples = %d, want the 320 spoken before preemption", got)
	}
	if owner := e.hub.AudioOwner(); owner != input.RemoteID {
		t.Fatalf("owner = %q, want the remote to keep the microphone", owner)
	}

	// Once the remote releases, a fresh utterance starts from a clean buffer.
	e.hub.ReleaseAudio(input.RemoteID)
	sendJSON(t, ctx, conn, map[string]any{"type": "start", "format": "pcm16", "rate": 16000, "channels": 1})
	if frame := readMessage(t, ctx, conn); frame["type"] != "listening" || frame["active"] != true {
		t.Fatalf("second start = %v", frame)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, pcmFrame(320)); err != nil {
		t.Fatal(err)
	}
	sendJSON(t, ctx, conn, map[string]any{"type": "end"})
	waitFor(t, func() bool { return len(transcriber.snapshot()) == 2 }, "second utterance should reach the chain")
	if got := transcriber.snapshot()[1].samples; got != 320 {
		t.Fatalf("second samples = %d, want 320 (stray frame survived)", got)
	}
}

// DOM-09: a capture cut at the server cap must be labelled as truncated when
// it reaches the shared chain — the same signal the phone page already gets.
func TestTruncatedUtteranceIsLabelledForTheChain(t *testing.T) {
	transcriber := &fakeTranscriber{ok: true, result: UtteranceResult{Text: "截断", At: time.Now()}}
	e := newTestEnv(t, ServerConfig{Transcriber: transcriber, MaxUtteranceSeconds: 1, MaxClients: 4})
	conn, ctx := dialPhone(t, e, "Pixel")

	sendJSON(t, ctx, conn, map[string]any{"type": "start", "format": "pcm16", "rate": 16000, "channels": 1})
	if frame := readMessage(t, ctx, conn); frame["type"] != "listening" {
		t.Fatalf("start = %v", frame)
	}
	// 1 s cap at 16 kHz = 16000 samples = 50 frames of 320.
	for i := 0; i < 52; i++ {
		if err := conn.Write(ctx, websocket.MessageBinary, pcmFrame(320)); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, func() bool { return len(transcriber.snapshot()) == 1 }, "capped utterance should reach the chain")
	call := transcriber.snapshot()[0]
	if !call.meta.Truncated {
		t.Fatalf("meta = %+v, want Truncated set", call.meta)
	}
}

// blockingTransport holds Inject open until released, like a wedged root
// helper.
type blockingTransport struct {
	entered chan struct{}
	release chan struct{}
}

func (b *blockingTransport) Inject(ctx context.Context, key string, value int) error {
	select {
	case b.entered <- struct{}{}:
	default:
	}
	<-b.release
	return nil
}

// DOM-07: a slow key injection must not stall the same connection's audio.
// The key channel is asynchronous and ordered; PCM keeps flowing meanwhile.
func TestSlowKeyInjectionDoesNotBlockAudio(t *testing.T) {
	transport := &blockingTransport{entered: make(chan struct{}, 1), release: make(chan struct{})}
	transcriber := &fakeTranscriber{ok: true, result: UtteranceResult{Text: "你好", At: time.Now()}}
	e := newTestEnv(t, ServerConfig{Transcriber: transcriber, MaxUtteranceSeconds: 30, MaxClients: 4})
	e.server.injector = NewKeyInjector(transport)
	conn, ctx := dialPhone(t, e, "Pixel")

	// A key press wedges inside the helper...
	sendJSON(t, ctx, conn, map[string]any{"type": "key", "key": "up", "value": 1})
	select {
	case <-transport.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("key injection never reached the transport")
	}

	// ...and the very next utterance must still be processed.
	sendJSON(t, ctx, conn, map[string]any{"type": "start", "format": "pcm16", "rate": 16000, "channels": 1})
	if frame := readMessage(t, ctx, conn); frame["type"] != "listening" || frame["active"] != true {
		t.Fatalf("start during stuck key = %v", frame)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, pcmFrame(320)); err != nil {
		t.Fatal(err)
	}
	sendJSON(t, ctx, conn, map[string]any{"type": "end"})
	waitFor(t, func() bool { return len(transcriber.snapshot()) == 1 },
		"audio must flow while a key injection is stuck")

	close(transport.release)
}
