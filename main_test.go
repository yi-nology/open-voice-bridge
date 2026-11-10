package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/agent"
	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/input"
	"github.com/yi-nology/open-voice-bridge/internal/phone"
	"github.com/yi-nology/open-voice-bridge/internal/text"
)

// stubEngine implements the full asr.Engine interface
// (Name/Local/Transcribe/Close).
type stubEngine struct {
	result   asr.Result
	err      error
	captured []asr.Audio
}

func (e *stubEngine) Name() string                { return "stub" }
func (e *stubEngine) Local() bool                 { return true }
func (e *stubEngine) Close(context.Context) error { return nil }

func (e *stubEngine) Transcribe(_ context.Context, audio asr.Audio) (asr.Result, error) {
	e.captured = append(e.captured, audio)
	return e.result, e.err
}

func TestPhoneSpeechMapsTheTranscript(t *testing.T) {
	engine := &stubEngine{result: asr.Result{
		Text:     "帮我写个测试",
		Provider: "stub",
		Latency:  1300 * time.Millisecond,
		Language: "zh",
	}}
	rules, err := text.New(text.Config{Dictionary: []text.Replacement{{From: "测试", To: "unittest"}}})
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	ag := agent.New(config.Config{MaxUtterance: 30}, engine, nil)
	ag.SetTextRules(rules)
	adapter := phoneSpeech{pipeline: ag.Speech()}

	result, ok := adapter.TranscribeUtterance(context.Background(), make([]int16, 16000), phone.UtteranceMeta{
		DeviceID: "phone-abc", Name: "Pixel", Address: "192.0.2.10",
	})
	if !ok {
		t.Fatal("a full utterance must transcribe")
	}
	if result.Text != "帮我写个unittest" {
		t.Fatalf("text = %q, want the dictionary rewrite applied", result.Text)
	}
	if result.RawText != "帮我写个测试" {
		t.Fatalf("raw_text = %q, want the ASR output unchanged", result.RawText)
	}
	if !result.Processed || result.Provider != "stub" || result.LatencyMS != 1300 {
		t.Fatalf("result = %+v", result)
	}
	if result.At.IsZero() {
		t.Fatal("result must carry a timestamp")
	}
	if len(engine.captured) != 1 || engine.captured[0].SampleRate != 16000 {
		t.Fatalf("engine received %+v, want one 16 kHz request", engine.captured)
	}
}

func TestPhoneSpeechDeclinesSilence(t *testing.T) {
	engine := &stubEngine{result: asr.Result{Text: "x", Provider: "stub"}}
	ag := agent.New(config.Config{MaxUtterance: 30}, engine, nil)
	if _, ok := (phoneSpeech{pipeline: ag.Speech()}).TranscribeUtterance(
		context.Background(), make([]int16, 100), phone.UtteranceMeta{Name: "Pixel"}); ok {
		t.Fatal("audio shorter than the 250 ms floor must not transcribe")
	}
	if len(engine.captured) != 0 {
		t.Fatal("ASR must not be called for silence")
	}
}

func TestSpeakingLabel(t *testing.T) {
	devices := []input.Status{
		{ID: input.RemoteID, Kind: input.KindRemote, Name: "小米遥控器", Speaking: false},
		{ID: "phone-abc", Kind: input.KindPhone, Name: "Pixel", Speaking: true},
	}
	if active, source, name := speakingLabel(input.AudioState{Owner: "phone-abc", Speaking: true}, devices); !active || source != "phone" || name != "Pixel" {
		t.Fatalf("phone owner mapped to (%v, %q, %q)", active, source, name)
	}
	if active, source, name := speakingLabel(input.AudioState{Owner: input.RemoteID, Speaking: true}, devices); !active || source != "remote" || !strings.Contains(name, "遥控") {
		t.Fatalf("remote owner mapped to (%v, %q, %q)", active, source, name)
	}
	if active, _, _ := speakingLabel(input.AudioState{}, devices); active {
		t.Fatal("an idle hub must not report a speaker")
	}
	// An owner we cannot resolve still reports that something is speaking;
	// the pill must not go quiet just because a label is missing.
	if active, source, name := speakingLabel(input.AudioState{Owner: "gone", Speaking: true}, devices); !active || source != "phone" || name != "gone" {
		t.Fatalf("unknown owner mapped to (%v, %q, %q)", active, source, name)
	}
}
