package runtime

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oekit/logx"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/yi-nology/open-voice-bridge/internal/agent"
	"github.com/yi-nology/open-voice-bridge/internal/history"
	"github.com/yi-nology/open-voice-bridge/internal/input"
)

type usageCall struct {
	speech time.Duration
	chars  int
}

type eventRecorder struct {
	mu        sync.Mutex
	entries   []history.Entry
	usage     []usageCall
	published chan map[string]any
}

func newEventRecorder() *eventRecorder {
	return &eventRecorder{published: make(chan map[string]any, 8)}
}

func (r *eventRecorder) Append(entry history.Entry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, entry)
	return nil
}

func (r *eventRecorder) Record(speech time.Duration, chars int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.usage = append(r.usage, usageCall{speech, chars})
}

func (r *eventRecorder) Publish(event map[string]any) { r.published <- event }

func (r *eventRecorder) next(t *testing.T) map[string]any {
	t.Helper()
	select {
	case event := <-r.published:
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("no event was published")
		return nil
	}
}

func (r *eventRecorder) snapshot() ([]history.Entry, []usageCall) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]history.Entry(nil), r.entries...), append([]usageCall(nil), r.usage...)
}

type eventFeeds struct {
	transcripts chan agent.Transcript
	microphone  chan input.AudioState
	phrases     chan agent.PhraseEvent
}

func testEvents(recorder *eventRecorder) (Events, eventFeeds) {
	feeds := eventFeeds{
		transcripts: make(chan agent.Transcript, 4),
		microphone:  make(chan input.AudioState, 4),
		phrases:     make(chan agent.PhraseEvent, 4),
	}
	return Events{
		Transcripts:      feeds.transcripts,
		Microphone:       feeds.microphone,
		Phrases:          feeds.phrases,
		Sources:          func() []input.Status { return nil },
		History:          recorder,
		Usage:            recorder,
		Publish:          recorder.Publish,
		TranscriptsTotal: prometheus.NewCounter(prometheus.CounterOpts{Name: "test_transcripts"}),
		ASRLatency:       prometheus.NewHistogram(prometheus.HistogramOpts{Name: "test_latency"}),
		Logger:           logx.NewSlogLogger("events-test", logx.WithHandler(slog.NewTextHandler(io.Discard, nil))),
	}, feeds
}

// A dictated sentence is the one stream that becomes durable: history, usage
// counters and the live subtitle all see it.
func TestEventsTranscriptReachesHistoryUsageAndSubtitle(t *testing.T) {
	recorder := newEventRecorder()
	events, feeds := testEvents(recorder)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { events.Run(ctx); close(done) }()

	at := time.Date(2026, 11, 12, 9, 30, 0, 0, time.UTC)
	feeds.transcripts <- agent.Transcript{
		Text: "你好世界", RawText: "你好 世界", Provider: "mimo", Language: "zh",
		Source: "remote", SourceName: "遥控器", Latency: 300 * time.Millisecond,
		Duration: 2 * time.Second, At: at,
	}

	published := recorder.next(t)
	if published["event"] != "transcript" || published["text"] != "你好世界" {
		t.Fatalf("published = %v", published)
	}
	if published["at"] != at.Format(time.RFC3339) {
		t.Errorf("at = %v, want RFC3339", published["at"])
	}
	if published["latency_ms"] != int64(300) {
		t.Errorf("latency_ms = %v, want 300", published["latency_ms"])
	}
	entries, usage := recorder.snapshot()
	if len(entries) != 1 || entries[0].Text != "你好世界" || entries[0].Provider != "mimo" {
		t.Fatalf("history = %+v", entries)
	}
	if !entries[0].At.Equal(at) {
		t.Errorf("history at = %v, want %v", entries[0].At, at)
	}
	if len(usage) != 1 || usage[0].speech != 2*time.Second || usage[0].chars != 4 {
		t.Fatalf("usage = %+v, want one call of 2s / 4 runes", usage)
	}
}

// Common phrases are user-authored text, not speech: they show up live but
// must never enter history or the usage counters.
func TestEventsPhraseIsLiveOnly(t *testing.T) {
	recorder := newEventRecorder()
	events, feeds := testEvents(recorder)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go events.Run(ctx)

	at := time.Date(2026, 11, 12, 9, 31, 0, 0, time.UTC)
	feeds.phrases <- agent.PhraseEvent{Slot: 3, Text: "收货地址", At: at}

	published := recorder.next(t)
	if published["event"] != "phrase" || published["slot"] != 3 || published["text"] != "收货地址" {
		t.Fatalf("published = %v", published)
	}
	if published["at"] != at.Format(time.RFC3339) {
		t.Errorf("at = %v, want RFC3339", published["at"])
	}
	entries, usage := recorder.snapshot()
	if len(entries) != 0 {
		t.Errorf("a phrase entered history: %+v", entries)
	}
	if len(usage) != 0 {
		t.Errorf("a phrase entered the usage counters: %+v", usage)
	}
}

// The pill names whoever holds the microphone, resolved through the hub's
// source list rather than the raw owner id.
func TestEventsListeningNamesTheOwner(t *testing.T) {
	recorder := newEventRecorder()
	events, feeds := testEvents(recorder)
	events.Sources = func() []input.Status {
		return []input.Status{
			{ID: input.RemoteID, Kind: input.KindRemote, Name: "小米遥控器"},
			{ID: "phone-abc", Kind: input.KindPhone, Name: "Pixel"},
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go events.Run(ctx)

	feeds.microphone <- input.AudioState{Speaking: true, Owner: "phone-abc"}
	published := recorder.next(t)
	if published["event"] != "listening" || published["active"] != true {
		t.Fatalf("published = %v", published)
	}
	if published["source"] != "phone" || published["source_name"] != "Pixel" {
		t.Errorf("listening label = %v/%v, want phone/Pixel", published["source"], published["source_name"])
	}

	feeds.microphone <- input.AudioState{}
	published = recorder.next(t)
	if published["active"] != false {
		t.Errorf("an idle hub published active = %v", published["active"])
	}
}

func TestEventsRunStopsWithItsContext(t *testing.T) {
	recorder := newEventRecorder()
	events, _ := testEvents(recorder)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { events.Run(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}
}

func TestSpeakingLabel(t *testing.T) {
	devices := []input.Status{
		{ID: input.RemoteID, Kind: input.KindRemote, Name: "小米遥控器", Speaking: false},
		{ID: "phone-abc", Kind: input.KindPhone, Name: "Pixel", Speaking: true},
	}
	if active, source, name := SpeakingLabel(input.AudioState{Owner: "phone-abc", Speaking: true}, devices); !active || source != "phone" || name != "Pixel" {
		t.Fatalf("phone owner mapped to (%v, %q, %q)", active, source, name)
	}
	if active, source, name := SpeakingLabel(input.AudioState{Owner: input.RemoteID, Speaking: true}, devices); !active || source != "remote" || !strings.Contains(name, "遥控") {
		t.Fatalf("remote owner mapped to (%v, %q, %q)", active, source, name)
	}
	if active, _, _ := SpeakingLabel(input.AudioState{}, devices); active {
		t.Fatal("an idle hub must not report a speaker")
	}
	// An owner we cannot resolve still reports that something is speaking;
	// the pill must not go quiet just because a label is missing.
	if active, source, name := SpeakingLabel(input.AudioState{Owner: "gone", Speaking: true}, devices); !active || source != "phone" || name != "gone" {
		t.Fatalf("unknown owner mapped to (%v, %q, %q)", active, source, name)
	}
}
