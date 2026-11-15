package speech

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/core"
	"github.com/yi-nology/open-voice-bridge/internal/llm"
	"github.com/yi-nology/open-voice-bridge/internal/text"
)

// fakeEngine is a stub ASR engine returning a canned result.
type fakeEngine struct {
	result asr.Result
	err    error
	calls  int
}

func (f *fakeEngine) Name() string                { return "fake" }
func (f *fakeEngine) Local() bool                 { return true }
func (f *fakeEngine) Close(context.Context) error { return nil }
func (f *fakeEngine) Transcribe(context.Context, asr.Audio) (asr.Result, error) {
	f.calls++
	return f.result, f.err
}

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

func buildPipeline(engine asr.Engine, rules core.TextRules, probe *pipelineProbe, pasteOn bool) *Pipeline {
	return &Pipeline{
		engine:       func() asr.Engine { return engine },
		rules:        func() core.TextRules { return rules },
		processor:    func() llm.Processor { return nil },
		pasteEnabled: func() bool { return pasteOn },
		pasteText:    probe.paste,
		sink:         probe.sink,
		asrTimeout:   5 * time.Second,
	}
}

// oneSecond is a second of audible square wave: real captures always carry
// some level, and an all-zero buffer is digital silence the pipeline drops.
func oneSecond() []int16 {
	samples := make([]int16, core.SampleRate)
	for index := range samples {
		if index%2 == 0 {
			samples[index] = 800
		} else {
			samples[index] = -800
		}
	}
	return samples
}

// utterance builds a 16 kHz capture, which is what every test here pretends the
// source negotiated.
func utterance(samples []int16, ref SourceRef) Utterance {
	return Utterance{Samples: samples, SampleRate: core.SampleRate, Ref: ref}
}

func TestPipelineTranscribeStampsSourceAndAppliesDictionary(t *testing.T) {
	engine := &fakeEngine{result: asr.Result{Text: "hello world", Provider: "fake", Latency: 80 * time.Millisecond}}
	rules, err := text.New(config.TextConfig{Dictionary: []config.Replacement{{From: "world", To: "世界"}}})
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
	if transcript.Source != "phone" {
		t.Fatalf("source kind = %q, want the utterance's Kind stamped through", transcript.Source)
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
	rules, err := text.New(config.TextConfig{})
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
	if _, ok := pipeline.Transcribe(context.Background(), utterance(make([]int16, core.SampleRate/8), SourceRef{})); ok {
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
	rules, err := text.New(config.TextConfig{})
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

func (blockingEngine) Name() string { return "blocking" }

func (blockingEngine) Local() bool { return true }

func (blockingEngine) Close(context.Context) error { return nil }

func (blockingEngine) Transcribe(ctx context.Context, _ asr.Audio) (asr.Result, error) {
	<-ctx.Done()
	return asr.Result{}, ctx.Err()
}

// CON-03: the chain must honour the caller's context. A provider that never
// answers used to pin the serial chain until a fixed timeout, and SIGINT
// could not cut it short.
func TestTranscribeHonorsCallerCancellation(t *testing.T) {
	rules, err := text.New(config.TextConfig{})
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
	p := &Pipeline{asrTimeout: 180 * time.Second}
	short := p.budgetFor(utterance(oneSecond(), SourceRef{}))
	if short != 28*time.Second {
		t.Fatalf("1s audio budget = %v, want 28s", short)
	}
	empty := p.budgetFor(utterance(nil, SourceRef{}))
	if empty != 20*time.Second {
		t.Fatalf("empty audio budget = %v, want the 20s floor", empty)
	}
	long := p.budgetFor(utterance(make([]int16, 40*core.SampleRate), SourceRef{}))
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

func (g *gateEngine) Name() string { return "gate" }

func (g *gateEngine) Local() bool { return true }

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
	rules, err := text.New(config.TextConfig{})
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

// Common phrases (SayAll 对标): a phrase action inserts the nth snippet
// through the same serialised paste as a transcript, so the two never
// interleave keystrokes. Unbound slots must be a no-op.
func TestInsertPhrasePastesTheNthSnippet(t *testing.T) {
	rules, err := text.New(config.TextConfig{Snippets: []config.Snippet{
		{Trigger: "确认", Text: "好的，确认执行。"},
		{Trigger: "继续", Text: "请继续。"},
	}})
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	probe := &pipelineProbe{}
	pipeline := buildPipeline(&fakeEngine{}, rules, probe, true)

	got, ok := pipeline.InsertPhrase(2)
	if !ok || got != "请继续。" {
		t.Fatalf("InsertPhrase(2) = %q,%v want 请继续。", got, ok)
	}
	_, pasted := probe.snapshot()
	if len(pasted) != 1 || pasted[0] != "请继续。" {
		t.Fatalf("pasted = %v, want the phrase text", pasted)
	}

	if _, ok := pipeline.InsertPhrase(3); ok {
		t.Fatal("slot 3 is unbound and must not report success")
	}
	_, pasted = probe.snapshot()
	if len(pasted) != 1 {
		t.Fatalf("unbound slot must not paste, got %v", pasted)
	}
	// Paste is off: the phrase resolves but nothing reaches the clipboard.
	quiet := buildPipeline(&fakeEngine{}, rules, &pipelineProbe{}, false)
	if _, ok := quiet.InsertPhrase(1); !ok {
		t.Fatal("phrase lookup succeeds even when paste is disabled")
	}
}

// DOM-09 / I8: the shared chain stamps who spoke and whether the capture was
// cut short, so subtitles and history stop showing an anonymous address.
func TestTranscriptStampsSourceIdentity(t *testing.T) {
	engine := &fakeEngine{result: asr.Result{Text: "你好", Provider: "fake"}}
	rules, err := text.New(config.TextConfig{})
	if err != nil {
		t.Fatal(err)
	}
	probe := &pipelineProbe{}
	pipeline := buildPipeline(engine, rules, probe, false)

	utterance := utterance(oneSecond(), SourceRef{Kind: "phone", Name: "客厅手机", Address: "192.0.2.10"})
	utterance.Truncated = true
	transcript, ok := pipeline.Transcribe(context.Background(), utterance)
	if !ok {
		t.Fatal("transcribe failed")
	}
	if transcript.Source != "phone" || transcript.SourceName != "客厅手机" {
		t.Fatalf("identity = %q/%q", transcript.Source, transcript.SourceName)
	}
	if !transcript.Truncated {
		t.Fatal("truncation must reach the transcript")
	}
}

// 语音命令: an exact command utterance runs its key combo and never becomes
// text — no paste — and the transcript carries the Command marker so the
// history sink skips it (like phrases, a spoken act).
func TestVoiceCommandRunsComboInsteadOfText(t *testing.T) {
	rules, err := text.New(config.TextConfig{
		CommandsEnabled: true,
		Commands:        map[string]string{"复制": "ctrl+c"},
	})
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	probe := &pipelineProbe{}
	engine := &fakeEngine{result: asr.Result{Text: "复制", Provider: "fake"}}
	pipeline := buildPipeline(engine, rules, probe, true)
	var combos []string
	pipeline.sendKeys = func(_ context.Context, combo string) error {
		combos = append(combos, combo)
		return nil
	}
	transcript, ok := pipeline.Transcribe(context.Background(), utterance(oneSecond(), SourceRef{Kind: "remote"}))
	if !ok {
		t.Fatal("command utterance was dropped")
	}
	if transcript.Command != "ctrl+c" || transcript.Text != "复制" {
		t.Fatalf("transcript = %+v, want the command marker", transcript)
	}
	if len(combos) != 1 || combos[0] != "ctrl+c" {
		t.Fatalf("combos = %v, want [ctrl+c]", combos)
	}
	transcripts, pasted := probe.snapshot()
	if len(pasted) != 0 {
		t.Fatalf("pasted %v, want nothing", pasted)
	}
	if len(transcripts) != 1 || transcripts[0].Command != "ctrl+c" {
		t.Fatalf("sink saw %+v", transcripts)
	}
}

// A command that fails to execute falls back to plain dictation so the words
// are never lost.
func TestVoiceCommandFallsBackToDictation(t *testing.T) {
	rules, err := text.New(config.TextConfig{
		CommandsEnabled: true,
		Commands:        map[string]string{"复制": "ctrl+c"},
	})
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	probe := &pipelineProbe{}
	engine := &fakeEngine{result: asr.Result{Text: "复制", Provider: "fake"}}
	pipeline := buildPipeline(engine, rules, probe, true)
	pipeline.sendKeys = func(_ context.Context, combo string) error {
		return errors.New("keyboard grab failed")
	}
	transcript, ok := pipeline.Transcribe(context.Background(), utterance(oneSecond(), SourceRef{Kind: "remote"}))
	if !ok {
		t.Fatal("fallback dictation was dropped")
	}
	if transcript.Command != "" {
		t.Fatalf("command marker set despite failure: %+v", transcript)
	}
	_, pasted := probe.snapshot()
	if len(pasted) != 1 || pasted[0] != "复制" {
		t.Fatalf("pasted %v, want the words as fallback", pasted)
	}
}

// S5: the app-aware mode rides into the processor; "off" keeps the words
// literal without touching it at all.
type modeProbe struct {
	modes []string
	texts []string
}

func (m *modeProbe) Process(_ context.Context, text string) (string, error) {
	return m.ProcessMode(context.Background(), text, "")
}

func (m *modeProbe) ProcessMode(_ context.Context, text, mode string) (string, error) {
	m.modes = append(m.modes, mode)
	m.texts = append(m.texts, text)
	return "【" + text + "】", nil
}

func (m *modeProbe) Name() string { return "mode-probe" }

func (m *modeProbe) Ping(context.Context) (time.Duration, error) {
	return 0, nil
}

func (m *modeProbe) Close(context.Context) error { return nil }

func TestRewriteModeRoutesPerApp(t *testing.T) {
	rules, err := text.New(config.TextConfig{})
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	probe := &pipelineProbe{}
	processor := &modeProbe{}
	pipeline := buildPipeline(&fakeEngine{result: asr.Result{Text: "喂你好呃那个帮我发个邮件", Provider: "fake"}}, rules, probe, false)
	pipeline.processor = func() llm.Processor { return processor }
	pipeline.rewriteMode = func() string { return "format" }
	transcript, ok := pipeline.Transcribe(context.Background(), utterance(oneSecond(), SourceRef{Kind: "remote"}))
	if !ok {
		t.Fatal("transcript dropped")
	}
	if len(processor.modes) != 1 || processor.modes[0] != "format" {
		t.Fatalf("modes = %v, want [format]", processor.modes)
	}
	if transcript.Text != "【喂你好呃那个帮我发个邮件】" {
		t.Fatalf("text = %q", transcript.Text)
	}

	// "off": no processor call, words stay literal.
	processor.modes = nil
	pipeline.rewriteMode = func() string { return "off" }
	transcript, ok = pipeline.Transcribe(context.Background(), utterance(oneSecond(), SourceRef{Kind: "remote"}))
	if !ok {
		t.Fatal("off-mode transcript dropped")
	}
	if len(processor.modes) != 0 {
		t.Fatalf("processor ran in off mode: %v", processor.modes)
	}
	if transcript.Text != "喂你好呃那个帮我发个邮件" {
		t.Fatalf("off text = %q", transcript.Text)
	}
}

// S6: partial recognition returns the raw text and touches neither paste,
// rules nor the transcript sink — the final pass owns all of that.
func TestTranscribePartialStaysOutOfTheWay(t *testing.T) {
	rules, err := text.New(config.TextConfig{Snippets: []config.Snippet{{Trigger: "地址", Text: "北京市海淀区"}}})
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	probe := &pipelineProbe{}
	engine := &fakeEngine{result: asr.Result{Text: "地址", Provider: "fake"}}
	pipeline := buildPipeline(engine, rules, probe, true)
	pipeline.rewriteMode = func() string { return "cleanup" }
	text, err := pipeline.TranscribePartial(context.Background(), utterance(oneSecond(), SourceRef{Kind: "remote"}))
	if err != nil {
		t.Fatal(err)
	}
	if text != "地址" {
		t.Fatalf("partial = %q, want the raw recognition (no snippet/rewrite)", text)
	}
	if _, pasted := probe.snapshot(); len(pasted) != 0 {
		t.Fatalf("pasted %v during partial", pasted)
	}
	transcripts, _ := probe.snapshot()
	if len(transcripts) != 0 {
		t.Fatalf("sink saw %+v during partial", transcripts)
	}
}

// A capture of near-silence must not reach the model. Cloud ASR hallucinates a
// short line on a fragment of dead air ("你好。", "1234567890"), so a mis-tap has
// to produce nothing instead of a confident wrong sentence.
func TestSilentCaptureIsNotTranscribed(t *testing.T) {
	engine := &fakeEngine{result: asr.Result{Text: "1234567890", Provider: "fake"}}
	probe := &pipelineProbe{}
	rules, err := text.New(config.TextConfig{})
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	pipeline := buildPipeline(engine, rules, probe, false)
	silent := make([]int16, core.MinUtterance(16000)*4)
	if _, ok := pipeline.Transcribe(context.Background(), Utterance{Samples: silent, SampleRate: 16000}); ok {
		t.Fatal("a silent capture must not produce a transcript")
	}
	if engine.calls != 0 {
		t.Fatalf("ASR calls = %d, want 0 for silence", engine.calls)
	}
	if transcripts, pasted := probe.snapshot(); len(transcripts) != 0 || len(pasted) != 0 {
		t.Fatalf("silence reached the sink/paste: %v %v", transcripts, pasted)
	}
}

func TestAudibleCaptureIsStillTranscribed(t *testing.T) {
	engine := &fakeEngine{result: asr.Result{Text: "ship it", Provider: "fake"}}
	samples := make([]int16, core.MinUtterance(16000)*4)
	for index := range samples {
		if index%2 == 0 {
			samples[index] = 600
		} else {
			samples[index] = -600
		}
	}
	rules, err := text.New(config.TextConfig{})
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	pipeline := buildPipeline(engine, rules, &pipelineProbe{}, false)
	if _, ok := pipeline.Transcribe(context.Background(), Utterance{Samples: samples, SampleRate: 16000}); !ok {
		t.Fatal("audible audio must still be transcribed")
	}
	if engine.calls != 1 {
		t.Fatalf("ASR calls = %d, want 1", engine.calls)
	}
}
