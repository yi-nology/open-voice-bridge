package agent

import (
	"context"
	"testing"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/text"
)

func TestWakeHitNormalizesSpeech(t *testing.T) {
	cases := []struct {
		heard string
		hit   bool
	}{
		{"小遥小遥", true},
		{"小遥小遥，", true},
		{"小姚小遥啊", false}, // homophone half: set the word to what the engine prints
		{"你好小遥小遥你好", true},
		{"今天天气不错", false},
		{"小遥", false}, // too short alone: the configured word governs
	}
	for _, tc := range cases {
		if got := wakeHit(tc.heard, "小遥小遥"); got != tc.hit {
			t.Fatalf("wakeHit(%q) = %v, want %v", tc.heard, got, tc.hit)
		}
	}
}

type scriptSource struct {
	frames chan []int16
}

func (s *scriptSource) Frames() <-chan []int16 { return s.frames }
func (s *scriptSource) Err() error             { return nil }
func (s *scriptSource) Close() error           { return nil }

// End-to-end over the wake loop: voiced frames wake it, the dictation after
// the wake phrase lands in the shared chain as a "wake" source.
func TestWakeLoopCapturesAfterWakeWord(t *testing.T) {
	rules, err := text.New(config.TextConfig{})
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	engine := &fakeEngine{result: asr.Result{Text: "小遥小遥", Provider: "fake"}}
	probe := &pipelineProbe{}
	a := New(config.Config{PasteEnabled: false, MaxUtterance: 30}, engine, fakeSink{healthy: true})
	a.pipeline = buildPipeline(engine, rules, probe, false)
	a.ctx = context.Background()
	a.maxSample = 30 * 16000
	source := &scriptSource{frames: make(chan []int16, 64)}

	a.wakeMu.Lock()
	a.startWakeLocked(source, "小遥小遥")
	a.wakeMu.Unlock()

	voice := make([]int16, 1600)
	for i := range voice {
		voice[i] = 6000
	}
	quiet := make([]int16, 1600)

	// Fill the rolling window (>=1s of voice).
	for i := 0; i < 12; i++ {
		source.frames <- voice
		time.Sleep(5 * time.Millisecond)
	}
	// The wake throttle is wall-clock: let it elapse, then one more voiced
	// frame fires the check and the async match lands.
	time.Sleep(950 * time.Millisecond)
	source.frames <- voice
	time.Sleep(150 * time.Millisecond)
	// More voice enters dictation mode once the match signal is picked up.
	for i := 0; i < 10; i++ {
		source.frames <- voice
		time.Sleep(5 * time.Millisecond)
	}
	// Silence ends the dictation — the deadline is wall-clock (600ms), so
	// one quiet frame opens it and the next one lands after it elapses.
	source.frames <- quiet
	time.Sleep(700 * time.Millisecond)
	source.frames <- quiet

	deadline := time.After(3 * time.Second)
	for {
		transcripts, _ := probe.snapshot()
		if len(transcripts) > 0 {
			if transcripts[0].Source != "wake" {
				t.Fatalf("source = %q, want wake", transcripts[0].Source)
			}
			if transcripts[0].Text == "" {
				t.Fatal("empty wake transcript")
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("no wake dictation reached the chain")
		case <-time.After(20 * time.Millisecond):
		}
	}
}
