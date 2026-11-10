package phone

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// gatedTranscriber blocks every call until release, so the queue can be filled
// deterministically.
type gatedTranscriber struct {
	mu      sync.Mutex
	calls   []fakeCall
	arrived chan struct{}
	release chan struct{}
}

func newGatedTranscriber() *gatedTranscriber {
	return &gatedTranscriber{arrived: make(chan struct{}, 1), release: make(chan struct{})}
}

func (g *gatedTranscriber) TranscribeUtterance(_ context.Context, samples []int16, meta UtteranceMeta) (UtteranceResult, bool) {
	g.mu.Lock()
	g.calls = append(g.calls, fakeCall{samples: len(samples), meta: meta})
	g.mu.Unlock()
	select {
	case g.arrived <- struct{}{}:
	default:
	}
	<-g.release
	return UtteranceResult{Text: "x", At: time.Now()}, true
}

func (g *gatedTranscriber) snapshot() []fakeCall {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]fakeCall(nil), g.calls...)
}

func (g *gatedTranscriber) waitArrived(t *testing.T) {
	t.Helper()
	select {
	case <-g.arrived:
	case <-time.After(3 * time.Second):
		t.Fatal("transcriber was never called")
	}
}

// speak sends one hold-to-talk utterance with the given frame count and
// returns the frame observed right after "end".
func speak(t *testing.T, ctx context.Context, conn *websocket.Conn, frames int) map[string]any {
	t.Helper()
	sendJSON(t, ctx, conn, map[string]any{"type": "start", "format": "pcm16", "rate": 16000, "channels": 1})
	if frame := readMessage(t, ctx, conn); frame["type"] != "listening" {
		t.Fatalf("start = %v", frame)
	}
	for i := 0; i < frames; i++ {
		if err := conn.Write(ctx, websocket.MessageBinary, pcmFrame(320)); err != nil {
			t.Fatalf("audio frame: %v", err)
		}
	}
	sendJSON(t, ctx, conn, map[string]any{"type": "end"})
	return readMessage(t, ctx, conn)
}

// SEC-08: utterances must reach ASR in capture order. Once one is in flight,
// the next two are queued; the old per-utterance goroutine raced and could
// swap them in the subtitles and history.
func TestPhoneTranscriptionIsOrdered(t *testing.T) {
	gated := newGatedTranscriber()
	e := newTestEnv(t, ServerConfig{Transcriber: gated, MaxUtteranceSeconds: 30, MaxClients: 4})
	conn, ctx := dialPhone(t, e, "Pixel")

	speak(t, ctx, conn, 1) // A: 320 samples, blocks in the worker
	gated.waitArrived(t)
	speak(t, ctx, conn, 2) // B: 640
	speak(t, ctx, conn, 3) // C: 960

	close(gated.release)
	waitFor(t, func() bool { return len(gated.snapshot()) == 3 }, "three utterances should be transcribed")
	calls := gated.snapshot()
	want := []int{320, 640, 960}
	for i, w := range want {
		if calls[i].samples != w {
			t.Fatalf("call %d samples = %d, want %d (order swapped: %v)", i, calls[i].samples, w, calls)
		}
	}
}

// SEC-08: the in-flight queue is bounded. When it is full a new hold-to-talk
// is refused with busy instead of pinning another 960 KB of PCM behind a slow
// provider.
func TestPhoneTranscriptionQueueIsBounded(t *testing.T) {
	gated := newGatedTranscriber()
	e := newTestEnv(t, ServerConfig{Transcriber: gated, MaxUtteranceSeconds: 30, MaxClients: 4, MaxQueuedUtterances: 2})
	conn, ctx := dialPhone(t, e, "Pixel")

	speak(t, ctx, conn, 1) // A runs in the worker and blocks
	gated.waitArrived(t)
	speak(t, ctx, conn, 2) // B queues (1/2)
	speak(t, ctx, conn, 3) // C queues (2/2)

	sendJSON(t, ctx, conn, map[string]any{"type": "start", "format": "pcm16", "rate": 16000, "channels": 1})
	frame := readMessage(t, ctx, conn)
	if frame["type"] != "error" || frame["code"] != "busy" {
		t.Fatalf("start with a full queue = %v, want busy", frame)
	}
	if calls := gated.snapshot(); len(calls) != 1 {
		t.Fatalf("transcriber calls = %d, want 1 in flight", len(calls))
	}

	close(gated.release)
	waitFor(t, func() bool { return len(gated.snapshot()) == 3 }, "queued utterances should drain")
}

// The direct enqueue contract: false when full, and the queue never grows past
// its cap.
func TestUtteranceQueueRejectsWhenFull(t *testing.T) {
	q := newUtteranceQueue(2)
	job := utteranceJob{samples: []int16{1}, meta: UtteranceMeta{DeviceID: "phone-1"}}
	if !q.offer(job) {
		t.Fatal("first offer rejected")
	}
	if !q.offer(job) {
		t.Fatal("second offer rejected")
	}
	if q.offer(job) {
		t.Fatal("third offer accepted past the cap")
	}
	if q.len() != 2 {
		t.Fatalf("len = %d, want 2", q.len())
	}
}
