package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/atvv"
	"github.com/yi-nology/open-voice-bridge/internal/llm"
	"github.com/yi-nology/open-voice-bridge/internal/text"
)

type pipelineProbe struct {
	transcripts []Transcript
	pasted      []string
	mu          sync.Mutex
}

func (p *pipelineProbe) sink(t Transcript) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.transcripts = append(p.transcripts, t)
}

func (p *pipelineProbe) paste(_ context.Context, s string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pasted = append(p.pasted, s)
	return nil
}

func (p *pipelineProbe) snapshot() ([]Transcript, []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Transcript(nil), p.transcripts...), append([]string(nil), p.pasted...)
}

func buildPipeline(engine asr.Engine, rules *text.Rules, probe *pipelineProbe, pasteOn bool) *SpeechPipeline {
	return &SpeechPipeline{
		engine:       func() asr.Engine { return engine },
		rules:        func() *text.Rules { return rules },
		processor:    func() llm.Processor { return nil },
		pasteEnabled: func() bool { return pasteOn },
		pasteText:    probe.paste,
		sink:         probe.sink,
		asrTimeout:   5 * time.Second,
	}
}

func oneSecond() []int16 { return make([]int16, atvv.SampleRate) }

// utterance builds a 16 kHz capture, which is what every test here pretends the
// source negotiated.
func utterance(samples []int16, ref SourceRef) Utterance {
	return Utterance{Samples: samples, SampleRate: atvv.SampleRate, Ref: ref}
}

func TestPipelineTranscribeStampsSourceAndAppliesDictionary(t *testing.T) {
	engine := &fakeEngine{result: asr.Result{Text: "hello world", Provider: "fake", Latency: 80 * time.Millisecond}}
	rules, err := text.New(text.Config{Dictionary: []text.Replacement{{From: "world", To: "世界"}}})
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	probe := &pipelineProbe{}
	pipeline := buildPipeline(engine, rules, probe, false)

	transcript, ok := pipeline.Transcribe(context.Background(), utterance(oneSecond(), SourceRef{Kind: "phone", Address: "192.0.2.10"}))
	if !ok {
		t.Fatal("expected transcript")
	}
	if transcript.Text != "hello 世界" || transcript.RawText != "hello world" || !transcript.Processed {
		t.Fatalf("transcript = %+v", transcript)
	}
	if transcript.Address != "192.0.2.10" {
		t.Fatalf("address = %q, want source address", transcript.Address)
	}
	saved, pasted := probe.snapshot()
	if len(saved) != 1 || saved[0].Address != "192.0.2.10" {
		t.Fatalf("sink transcripts = %+v", saved)
	}
	if len(pasted) != 0 {
		t.Fatalf("paste disabled but pasted = %v", pasted)
	}
}

func TestPipelinePasteEnabled(t *testing.T) {
	engine := &fakeEngine{result: asr.Result{Text: "ship it", Provider: "fake"}}
	rules, err := text.New(text.Config{})
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	probe := &pipelineProbe{}
	pipeline := buildPipeline(engine, rules, probe, true)
	if _, ok := pipeline.Transcribe(context.Background(), utterance(oneSecond(), SourceRef{Kind: "remote"})); !ok {
		t.Fatal("expected transcript")
	}
	_, pasted := probe.snapshot()
	if len(pasted) != 1 || pasted[0] != "ship it" {
		t.Fatalf("pasted = %v", pasted)
	}
}

func TestPipelineTooShortSkipsEngine(t *testing.T) {
	engine := &fakeEngine{result: asr.Result{Text: "hi"}}
	probe := &pipelineProbe{}
	pipeline := buildPipeline(engine, nil, probe, false)
	if _, ok := pipeline.Transcribe(context.Background(), utterance(make([]int16, atvv.SampleRate/8), SourceRef{})); ok {
		t.Fatal("short audio must not produce transcript")
	}
	if engine.calls != 0 {
		t.Fatalf("short audio must not call ASR, calls = %d", engine.calls)
	}
}

func TestPipelineNilEngine(t *testing.T) {
	probe := &pipelineProbe{}
	pipeline := buildPipeline(nil, nil, probe, false)
	if _, ok := pipeline.Transcribe(context.Background(), utterance(oneSecond(), SourceRef{})); ok {
		t.Fatal("nil engine must not produce transcript")
	}
}

func TestPipelineNoSpeechDoesNotPublish(t *testing.T) {
	engine := &fakeEngine{err: asr.ErrNoSpeech}
	probe := &pipelineProbe{}
	pipeline := buildPipeline(engine, nil, probe, false)
	if _, ok := pipeline.Transcribe(context.Background(), utterance(oneSecond(), SourceRef{})); ok {
		t.Fatal("no-speech must not produce transcript")
	}
	if saved, _ := probe.snapshot(); len(saved) != 0 {
		t.Fatalf("sink should be idle: %+v", saved)
	}
}

// TestPipelineReusedByTwoSources proves the extraction goal: one chain serves
// both the remote and phone paths, each labelled with its own source.
func TestPipelineReusedByTwoSources(t *testing.T) {
	engine := &fakeEngine{result: asr.Result{Text: "ok", Provider: "fake"}}
	rules, err := text.New(text.Config{})
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	probe := &pipelineProbe{}
	pipeline := buildPipeline(engine, rules, probe, false)

	if _, ok := pipeline.Transcribe(context.Background(), utterance(oneSecond(), SourceRef{Kind: "remote", Address: "AA:BB:CC:DD:EE:FF"})); !ok {
		t.Fatal("remote utterance expected")
	}
	if _, ok := pipeline.Transcribe(context.Background(), utterance(oneSecond(), SourceRef{Kind: "phone", Address: "192.0.2.10"})); !ok {
		t.Fatal("phone utterance expected")
	}
	saved, _ := probe.snapshot()
	if len(saved) != 2 {
		t.Fatalf("transcripts = %d, want 2", len(saved))
	}
	if !strings.EqualFold(saved[0].Address, "AA:BB:CC:DD:EE:FF") || saved[1].Address != "192.0.2.10" {
		t.Fatalf("source labelling wrong: %+v", saved)
	}
}

// blockingEngine holds the ASR call open until its context ends.
type blockingEngine struct{}

func (blockingEngine) Name() string                { return "blocking" }
func (blockingEngine) Local() bool                 { return true }
func (blockingEngine) Close(context.Context) error { return nil }
func (blockingEngine) Transcribe(ctx context.Context, _ asr.Audio) (asr.Result, error) {
	<-ctx.Done()
	return asr.Result{}, ctx.Err()
}

// CON-03: the chain must honour the caller's context. A provider that never
// answers used to pin the serial chain until a fixed timeout, and SIGINT
// could not cut it short.
func TestTranscribeHonorsCallerCancellation(t *testing.T) {
	rules, err := text.New(text.Config{})
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	probe := &pipelineProbe{}
	pipeline := buildPipeline(blockingEngine{}, rules, probe, false)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	done := make(chan bool, 1)
	go func() {
		_, ok := pipeline.Transcribe(ctx, utterance(oneSecond(), SourceRef{Kind: "remote"}))
		done <- ok
	}()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("cancelled transcription must not publish")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Transcribe ignored caller cancellation: ASR runs on context.Background()")
	}
}

// CON-03: the deadline scales with the captured audio — a one-second tap must
// not inherit the whole long-dictation window.
func TestTranscribeBudgetScalesWithAudio(t *testing.T) {
	p := &SpeechPipeline{asrTimeout: 180 * time.Second}
	short := p.budgetFor(utterance(oneSecond(), SourceRef{}))
	if short != 28*time.Second {
		t.Fatalf("1s audio budget = %v, want 28s", short)
	}
	empty := p.budgetFor(utterance(nil, SourceRef{}))
	if empty != 20*time.Second {
		t.Fatalf("empty audio budget = %v, want the 20s floor", empty)
	}
	long := p.budgetFor(utterance(make([]int16, 40*atvv.SampleRate), SourceRef{}))
	if long != 180*time.Second {
		t.Fatalf("40s audio budget = %v, want the 180s cap", long)
	}
}

// gateEngine blocks inside the first Transcribe call until release is closed,
// and answers every later call immediately. It stands in for a wedged cloud
// provider sharing the chain with a healthy second source.
type gateEngine struct {
	mu      sync.Mutex
	calls   int
	entered chan struct{}
	release chan struct{}
}

func (g *gateEngine) Name() string                { return "gate" }
func (g *gateEngine) Local() bool                 { return true }
func (g *gateEngine) Close(context.Context) error { return nil }
func (g *gateEngine) Transcribe(ctx context.Context, _ asr.Audio) (asr.Result, error) {
	g.mu.Lock()
	g.calls++
	n := g.calls
	g.mu.Unlock()
	if n == 1 {
		close(g.entered)
		select {
		case <-g.release:
		case <-ctx.Done():
			return asr.Result{}, ctx.Err()
		}
	}
	return asr.Result{Text: "ok", Provider: "gate"}, nil
}

// CON-03: one wedged provider must not pin the shared chain. The second
// source's utterance has to be recognised while the first is still stuck in
// ASR — holding p.mu across the request used to make every other source wait
// for the hung call's full deadline (up to asrTimeout).
func TestHungProviderDoesNotBlockOtherSources(t *testing.T) {
	engine := &gateEngine{entered: make(chan struct{}), release: make(chan struct{})}
	rules, err := text.New(text.Config{})
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	probe := &pipelineProbe{}
	pipeline := buildPipeline(engine, rules, probe, false)

	first := make(chan struct{})
	go func() {
		defer close(first)
		pipeline.Transcribe(context.Background(), utterance(oneSecond(), SourceRef{Kind: "remote"}))
	}()

	select {
	case <-engine.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first call never reached the engine")
	}

	second := make(chan bool, 1)
	go func() {
		_, ok := pipeline.Transcribe(context.Background(), utterance(oneSecond(), SourceRef{Kind: "phone"}))
		second <- ok
	}()

	select {
	case ok := <-second:
		if !ok {
			t.Fatal("second source should be recognised while the first is stuck in ASR")
		}
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("second source is blocked behind a hung ASR call: the request still holds the pipeline lock (CON-03)")
	}
	close(engine.release)
	select {
	case <-first:
	case <-time.After(2 * time.Second):
		t.Fatal("first call never returned after release")
	}
}
