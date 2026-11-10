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
		minSamples:   atvv.SampleRate / 4,
		asrTimeout:   5 * time.Second,
	}
}

func oneSecond() []int16 { return make([]int16, atvv.SampleRate) }

func TestPipelineTranscribeStampsSourceAndAppliesDictionary(t *testing.T) {
	engine := &fakeEngine{result: asr.Result{Text: "hello world", Provider: "fake", Latency: 80 * time.Millisecond}}
	rules, err := text.New(text.Config{Dictionary: []text.Replacement{{From: "world", To: "世界"}}})
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	probe := &pipelineProbe{}
	pipeline := buildPipeline(engine, rules, probe, false)

	transcript, ok := pipeline.Transcribe(oneSecond(), SourceRef{Kind: "phone", Address: "192.0.2.10"})
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
	if _, ok := pipeline.Transcribe(oneSecond(), SourceRef{Kind: "remote"}); !ok {
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
	if _, ok := pipeline.Transcribe(make([]int16, atvv.SampleRate/8), SourceRef{}); ok {
		t.Fatal("short audio must not produce transcript")
	}
	if engine.calls != 0 {
		t.Fatalf("short audio must not call ASR, calls = %d", engine.calls)
	}
}

func TestPipelineNilEngine(t *testing.T) {
	probe := &pipelineProbe{}
	pipeline := buildPipeline(nil, nil, probe, false)
	if _, ok := pipeline.Transcribe(oneSecond(), SourceRef{}); ok {
		t.Fatal("nil engine must not produce transcript")
	}
}

func TestPipelineNoSpeechDoesNotPublish(t *testing.T) {
	engine := &fakeEngine{err: asr.ErrNoSpeech}
	probe := &pipelineProbe{}
	pipeline := buildPipeline(engine, nil, probe, false)
	if _, ok := pipeline.Transcribe(oneSecond(), SourceRef{}); ok {
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

	if _, ok := pipeline.Transcribe(oneSecond(), SourceRef{Kind: "remote", Address: "AA:BB:CC:DD:EE:FF"}); !ok {
		t.Fatal("remote utterance expected")
	}
	if _, ok := pipeline.Transcribe(oneSecond(), SourceRef{Kind: "phone", Address: "192.0.2.10"}); !ok {
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
