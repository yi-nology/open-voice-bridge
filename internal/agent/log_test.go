package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oekit/logx"

	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/llm"
	"github.com/yi-nology/open-voice-bridge/internal/text"
)

// spoken is the sentence every log test pretends the user said. It must never
// appear in the log sink, whatever stage of the chain fails (SEC-02).
const spoken = "把客厅的灯关掉然后拉上窗帘"

// logSink collects the lines the process would have written, so a test can
// prove the transcript never reaches agent.log.
type logSink struct {
	mu   sync.Mutex
	body strings.Builder
}

func (s *logSink) Enabled(context.Context, slog.Level) bool { return true }

func (s *logSink) Handle(_ context.Context, record slog.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.body.WriteString(record.Message)
	record.Attrs(func(attr slog.Attr) bool {
		fmt.Fprintf(&s.body, " %s=%v", attr.Key, attr.Value.Any())
		return true
	})
	s.body.WriteString("\n")
	return nil
}

func (s *logSink) WithAttrs([]slog.Attr) slog.Handler { return s }
func (s *logSink) WithGroup(string) slog.Handler      { return s }

func (s *logSink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.body.String()
}

// captureLogs redirects the default logger for one test. Every package logs
// through logx.Delegating, which resolves the default per call, so swapping the
// default captures all of them.
func captureLogs(t *testing.T) *logSink {
	t.Helper()
	sink := &logSink{}
	previous := logx.Default()
	logx.SetDefault(logx.NewSlogLogger("test", logx.WithHandler(sink)))
	t.Cleanup(func() { logx.SetDefault(previous) })
	return sink
}

func assertNoTranscriptInLogs(t *testing.T, sink *logSink, stage string) {
	t.Helper()
	if got := sink.String(); strings.Contains(got, spoken) {
		t.Fatalf("%s: transcript text reached the log sink:\n%s", stage, got)
	}
}

func TestRecognizedLineLogsCountsNotText(t *testing.T) {
	sink := captureLogs(t)
	engine := &fakeEngine{result: asr.Result{
		Text:     spoken,
		Language: "zh",
		Provider: "mimo",
		Latency:  820 * time.Millisecond,
	}}
	rules, err := text.New(text.Config{})
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	probe := &pipelineProbe{}
	pipeline := buildPipeline(engine, rules, probe, false)

	if _, ok := pipeline.Transcribe(context.Background(), utterance(oneSecond(), SourceRef{Kind: "phone", Address: "192.0.2.10"})); !ok {
		t.Fatal("expected a transcript")
	}
	assertNoTranscriptInLogs(t, sink, "recognized")

	got := sink.String()
	// The support value has to survive the redaction, otherwise the line is
	// useless and someone will re-add the text to get it back.
	wants := []string{
		fmt.Sprintf("chars=%d", len([]rune(spoken))),
		"language=zh",
		"provider=mimo",
		"source=phone",
		"latency=820ms",
	}
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Fatalf("log line missing %q, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "192.0.2.10") {
		t.Fatalf("log line carries the source address, got:\n%s", got)
	}
}

// TestFailedLLMRequestLogsNoText covers the other way the sentence can leak:
// the chain logs the error, so an error that carries provider prose - which
// quotes the text it rejected - would print the transcript.
func TestFailedLLMRequestLogsNoText(t *testing.T) {
	sink := captureLogs(t)
	engine := &fakeEngine{result: asr.Result{Text: spoken, Provider: "fake", Latency: 10 * time.Millisecond}}
	rules, err := text.New(text.Config{})
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	probe := &pipelineProbe{}
	pipeline := buildPipeline(engine, rules, probe, false)
	pipeline.processor = func() llm.Processor {
		return failingProcessor{fmt.Errorf("llm HTTP 400: rejected: %s", spoken)}
	}

	if _, ok := pipeline.Transcribe(context.Background(), utterance(oneSecond(), SourceRef{Kind: "remote"})); !ok {
		t.Fatal("expected a transcript")
	}
	assertNoTranscriptInLogs(t, sink, "llm failure")
}

type failingProcessor struct{ err error }

func (f failingProcessor) Process(context.Context, string) (string, error) { return "", f.err }
func (f failingProcessor) Name() string                                    { return "failing" }
func (f failingProcessor) Ping(context.Context) (time.Duration, error)     { return 0, f.err }
func (f failingProcessor) Close(context.Context) error                     { return nil }

func TestAddressSwitchLogsMaskedAddress(t *testing.T) {
	sink := captureLogs(t)
	if err := (&Agent{}).SetAddress("AA:BB:CC:DD:EE:FF"); err != nil {
		t.Fatalf("SetAddress: %v", err)
	}
	got := sink.String()
	if strings.Contains(got, "AA:BB:CC:DD:EE:FF") {
		t.Fatalf("full BLE address logged:\n%s", got)
	}
	if !strings.Contains(got, "AA:BB:CC:**:**:FF") {
		t.Fatalf("masked address missing:\n%s", got)
	}
}
