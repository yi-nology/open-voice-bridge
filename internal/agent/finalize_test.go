package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/core"
	"github.com/yi-nology/open-voice-bridge/internal/text"
)

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

func newFinalizeAgent(engine asr.Engine) *Agent {
	return New(config.Config{PasteEnabled: false, MaxUtterance: 30}, engine, fakeSink{healthy: true})
}

func fillDictation(a *Agent, seconds int) {
	a.dictation = make([]int16, seconds*core.SampleRate)
}

func assertNoTranscript(t *testing.T, a *Agent) {
	t.Helper()
	select {
	case transcript := <-a.Transcripts():
		t.Fatalf("unexpected transcript: %+v", transcript)
	default:
	}
}

func TestFinalizePublishesProcessedTranscript(t *testing.T) {
	engine := &fakeEngine{result: asr.Result{
		Text: "hello world", Provider: "fake", Language: "en", Latency: 120 * time.Millisecond,
	}}
	a := newFinalizeAgent(engine)
	rules, err := text.New(config.TextConfig{Dictionary: []config.Replacement{{From: "world", To: "世界"}}})
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	a.SetTextRules(rules)
	fillDictation(a, 1)

	a.finalize()

	select {
	case transcript := <-a.Transcripts():
		if transcript.Text != "hello 世界" || transcript.RawText != "hello world" {
			t.Fatalf("text = %q raw = %q", transcript.Text, transcript.RawText)
		}
		if !transcript.Processed || transcript.Provider != "fake" || transcript.Latency != 120*time.Millisecond {
			t.Fatalf("transcript = %+v", transcript)
		}
	default:
		t.Fatal("no transcript published")
	}
	if engine.calls != 1 {
		t.Fatalf("engine calls = %d", engine.calls)
	}
}

func TestFinalizeAppliesSnippet(t *testing.T) {
	engine := &fakeEngine{result: asr.Result{Text: "邮箱", Provider: "fake"}}
	a := newFinalizeAgent(engine)
	rules, err := text.New(config.TextConfig{Snippets: []config.Snippet{{Trigger: "邮箱", Text: "me@example.com"}}})
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	a.SetTextRules(rules)
	fillDictation(a, 1)

	a.finalize()

	select {
	case transcript := <-a.Transcripts():
		if transcript.Text != "me@example.com" || !transcript.Processed {
			t.Fatalf("transcript = %+v", transcript)
		}
	default:
		t.Fatal("no transcript published")
	}
}

func TestFinalizeNoSpeech(t *testing.T) {
	engine := &fakeEngine{err: asr.ErrNoSpeech}
	a := newFinalizeAgent(engine)
	fillDictation(a, 1)
	a.finalize()
	assertNoTranscript(t, a)
	if engine.calls != 1 {
		t.Fatalf("engine calls = %d", engine.calls)
	}
}

func TestFinalizeShortAudioSkipsASR(t *testing.T) {
	engine := &fakeEngine{result: asr.Result{Text: "hi", Provider: "fake"}}
	a := newFinalizeAgent(engine)
	a.dictation = make([]int16, core.SampleRate/8)
	a.finalize()
	assertNoTranscript(t, a)
	if engine.calls != 0 {
		t.Fatalf("short audio must not call ASR, calls = %d", engine.calls)
	}
}

func TestFinalizeASRError(t *testing.T) {
	engine := &fakeEngine{err: errors.New("boom")}
	a := newFinalizeAgent(engine)
	fillDictation(a, 1)
	a.finalize()
	assertNoTranscript(t, a)
}

func TestFinalizeEmptyText(t *testing.T) {
	engine := &fakeEngine{result: asr.Result{Text: "", Provider: "fake"}}
	a := newFinalizeAgent(engine)
	fillDictation(a, 1)
	a.finalize()
	assertNoTranscript(t, a)
}
