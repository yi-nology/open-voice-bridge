package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/core"
	"github.com/yi-nology/open-voice-bridge/internal/llm"
	"github.com/yi-nology/open-voice-bridge/internal/remap"
	"github.com/yi-nology/open-voice-bridge/internal/speech"
	"github.com/yi-nology/open-voice-bridge/internal/text"
)

// phraseStub feeds phrase slots to the watcher without any evdev.
type phraseStub struct {
	ch       chan int
	pickerCh chan struct{}
	actionCh chan int
}

func (p *phraseStub) Start(context.Context) error { return nil }
func (p *phraseStub) Close(context.Context) error { return nil }
func (p *phraseStub) C() <-chan struct{}          { return nil }
func (p *phraseStub) Phrases() <-chan int         { return p.ch }
func (p *phraseStub) Actions() <-chan int {
	if p.actionCh == nil {
		p.actionCh = make(chan int)
	}
	return p.actionCh
}

func (p *phraseStub) Picker() <-chan struct{} {
	if p.pickerCh == nil {
		p.pickerCh = make(chan struct{})
	}
	return p.pickerCh
}

// pasteSpy records what the pipeline would paste.
type pasteSpy struct {
	mu     sync.Mutex
	pasted []string
}

func (p *pasteSpy) paste(_ context.Context, text string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pasted = append(p.pasted, text)
	return nil
}

func (p *pasteSpy) snapshot() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.pasted...)
}

// captureSpy records the transcripts and pastes a pipeline produces.
type captureSpy struct {
	mu          sync.Mutex
	transcripts []speech.Transcript
	pasted      []string
}

func (c *captureSpy) sink(t speech.Transcript) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.transcripts = append(c.transcripts, t)
}

func (c *captureSpy) snapshot() []speech.Transcript {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]speech.Transcript(nil), c.transcripts...)
}

func (c *captureSpy) paste(_ context.Context, text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pasted = append(c.pasted, text)
	return nil
}

// testUtterance builds one second of silence at the default rate.
func testUtterance(ref speech.SourceRef) speech.Utterance {
	samples := make([]int16, core.SampleRate)
	for index := range samples {
		if index%2 == 0 {
			samples[index] = 800
		} else {
			samples[index] = -800
		}
	}
	return speech.Utterance{Samples: samples, SampleRate: core.SampleRate, Ref: ref}
}

// testPipeline assembles the shared chain with spies for agent-side tests.
func testPipeline(engine asr.Engine, rules core.TextRules, processor llm.Processor, spy *captureSpy) *speech.Pipeline {
	return speech.New(speech.Options{
		Engine:       func() asr.Engine { return engine },
		Rules:        func() core.TextRules { return rules },
		Processor:    func() llm.Processor { return processor },
		PasteEnabled: func() bool { return false },
		PasteText:    spy.paste,
		Sink:         spy.sink,
		ASRTimeout:   5 * time.Second,
	})
}

func watcherPipeline(rules *text.Rules, spy *pasteSpy) *speech.Pipeline {
	return speech.New(speech.Options{
		Engine:       func() asr.Engine { return nil },
		Rules:        func() core.TextRules { return rules },
		PasteEnabled: func() bool { return true },
		PasteText:    spy.paste,
		ASRTimeout:   5 * time.Second,
	})
}

// End to end over the watcher: a phrase key-down pastes the phrase and
// surfaces a notice for the UI, while an unbound slot stays silent.
func TestPhraseWatcherInsertsAndNotifies(t *testing.T) {
	rules, err := text.New(config.TextConfig{Snippets: []config.Snippet{
		{Trigger: "确认", Text: "好的，确认执行。"},
	}})
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	spy := &pasteSpy{}
	stub := &phraseStub{ch: make(chan int, 4)}
	watcher := NewPhraseWatcher(stub, watcherPipeline(rules, spy))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		watcher.Run(ctx)
	}()

	stub.ch <- 1
	select {
	case event := <-watcher.Events():
		if event.Slot != 1 || event.Text != "好的，确认执行。" {
			t.Fatalf("event = %+v", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no phrase event after a bound key")
	}
	if pasted := spy.snapshot(); len(pasted) != 1 {
		t.Fatalf("pasted = %v", pasted)
	}

	stub.ch <- 8
	select {
	case event := <-watcher.Events():
		t.Fatalf("unbound slot must not notify, got %+v", event)
	case <-time.After(150 * time.Millisecond):
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("phrase watcher did not stop")
	}
}

// S3: deferred type()/open() steps run on the agent side — the daemon only
// presses the synthetic key. Empty slots stay silent.
func TestSeqActionWatcherRunsDeferredSteps(t *testing.T) {
	stub := &phraseStub{ch: make(chan int)}
	stub.actionCh = make(chan int, 4)
	registry := map[int]remap.AgentAction{
		0: {Kind: "type", Arg: "你好"},
		1: {Kind: "open", Arg: "firefox"},
	}
	var mu sync.Mutex
	var pasted, launched []string
	watcher := NewSeqActionWatcher(stub, func(slot int) (remap.AgentAction, bool) {
		action, ok := registry[slot]
		return action, ok
	})
	watcher.pasteText = func(_ context.Context, text string) error {
		mu.Lock()
		defer mu.Unlock()
		pasted = append(pasted, text)
		return nil
	}
	watcher.launch = func(_ context.Context, name string) error {
		mu.Lock()
		defer mu.Unlock()
		launched = append(launched, name)
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watcher.Start(ctx)
	stub.actionCh <- 0
	stub.actionCh <- 9 // no such slot: silent no-op
	stub.actionCh <- 1
	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		done := len(pasted) >= 1 && len(launched) >= 1
		mu.Unlock()
		if done {
			break
		}
		select {
		case <-deadline:
			mu.Lock()
			t.Fatalf("pasted=%v launched=%v", pasted, launched)
		case <-time.After(10 * time.Millisecond):
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if pasted[0] != "你好" || launched[0] != "firefox" {
		t.Fatalf("pasted=%v launched=%v", pasted, launched)
	}
}
