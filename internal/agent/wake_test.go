package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/input"
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
	spy := &captureSpy{}
	a := New(config.Config{PasteEnabled: false, MaxUtterance: 30}, engine, fakeSink{healthy: true})
	a.pipeline = testPipeline(engine, rules, nil, spy)
	a.ctx = context.Background()
	a.maxSample = 30 * 16000
	source := &scriptSource{frames: make(chan []int16, 64)}

	a.wakeMu.Lock()
	a.wakeWord = "小遥小遥"
	a.wakeOn = true
	a.startDesktopLocked(source)
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
		transcripts := spy.snapshot()
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

// TestWakeLoopYieldsToTheVoiceButton (P2-2): the wake capture claims the mic
// as its own hub source, so the voice button's force-claim preempts it and
// the half-sentence is dropped — the two captures may never interleave text
// in the focused window, and a wake capture may never steal the mic from a
// phone (it acquires, it does not force).
func TestWakeLoopYieldsToTheVoiceButton(t *testing.T) {
	rules, err := text.New(config.TextConfig{})
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	engine := &fakeEngine{result: asr.Result{Text: "小遥小遥", Provider: "fake"}}
	spy := &captureSpy{}
	a := New(config.Config{PasteEnabled: false, MaxUtterance: 30}, engine, fakeSink{healthy: true})
	a.pipeline = testPipeline(engine, rules, nil, spy)
	a.ctx = context.Background()
	a.maxSample = 30 * 16000
	source := &scriptSource{frames: make(chan []int16, 64)}
	a.SetHub(input.NewHub())

	a.wakeMu.Lock()
	a.wakeWord = "小遥小遥"
	a.wakeOn = true
	a.startDesktopLocked(source)
	a.wakeMu.Unlock()

	voice := make([]int16, 1600)
	for i := range voice {
		voice[i] = 6000
	}
	quiet := make([]int16, 1600)

	for i := 0; i < 12; i++ {
		source.frames <- voice
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(950 * time.Millisecond)
	source.frames <- voice
	time.Sleep(150 * time.Millisecond)
	// The wake match has landed and the dictation is running — the button
	// takes the mic out from under it.
	a.acquireMic()
	for i := 0; i < 10; i++ {
		source.frames <- voice
		time.Sleep(5 * time.Millisecond)
	}
	source.frames <- quiet
	time.Sleep(700 * time.Millisecond)
	source.frames <- quiet
	time.Sleep(300 * time.Millisecond)

	if got := spy.snapshot(); len(got) != 0 {
		t.Fatalf("preempted wake capture still transcribed: %+v", got)
	}
}

// countingEngine counts recognitions and can hold one in flight.
type countingEngine struct {
	mu      sync.Mutex
	calls   int
	started chan struct{}
	gate    chan struct{}
}

func (e *countingEngine) Name() string                { return "counting" }
func (e *countingEngine) Local() bool                 { return true }
func (e *countingEngine) Close(context.Context) error { return nil }
func (e *countingEngine) Transcribe(context.Context, asr.Audio) (asr.Result, error) {
	e.mu.Lock()
	e.calls++
	e.mu.Unlock()
	if e.started != nil {
		select {
		case e.started <- struct{}{}:
		default:
		}
	}
	if e.gate != nil {
		<-e.gate
	}
	return asr.Result{Text: "今天天气不错", Provider: "counting"}, nil
}

func (e *countingEngine) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

func startWakeLoop(t *testing.T, engine asr.Engine) (*Agent, *scriptSource) {
	t.Helper()
	rules, err := text.New(config.TextConfig{})
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	spy := &captureSpy{}
	a := New(config.Config{PasteEnabled: false, MaxUtterance: 30}, engine, fakeSink{healthy: true})
	a.pipeline = testPipeline(engine, rules, nil, spy)
	a.ctx = context.Background()
	a.maxSample = 30 * 16000
	source := &scriptSource{frames: make(chan []int16, 256)}
	a.SetHub(input.NewHub())
	a.wakeMu.Lock()
	a.wakeWord = "小遥小遥"
	a.wakeOn = true
	a.startDesktopLocked(source)
	a.wakeMu.Unlock()
	return a, source
}

func voicedFrames(n int) [][]int16 {
	voice := make([]int16, 1600)
	for i := range voice {
		voice[i] = 6000
	}
	out := make([][]int16, n)
	for i := range out {
		out[i] = voice
	}
	return out
}

// stubSource is the hub face of a competing capture (a phone holding the mic).
type stubSource struct{ id string }

func (s *stubSource) ID() string       { return s.id }
func (s *stubSource) Kind() input.Kind { return input.KindPhone }
func (s *stubSource) Status() input.Status {
	return input.Status{ID: s.id, Kind: input.KindPhone, Name: s.id, Connected: true, Speaking: true}
}

// A wake hit during someone else's capture is dropped by acquireWakeMic, so
// spending an ASR call to produce it is pure waste — the checks pause while
// the microphone is claimed and resume when it is free.
func TestWakeSkipsRecognitionWhileTheMicrophoneIsBusy(t *testing.T) {
	engine := &countingEngine{}
	a, source := startWakeLoop(t, engine)

	hub := a.currentHub()
	hub.Register(&stubSource{id: "phone"})
	if !hub.AcquireAudio("phone") {
		t.Fatal("phone should hold the mic")
	}

	for _, frame := range voicedFrames(20) {
		source.frames <- frame
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(950 * time.Millisecond)
	source.frames <- voicedFrames(1)[0]
	time.Sleep(200 * time.Millisecond)

	if got := engine.count(); got != 0 {
		t.Fatalf("wake ran %d recognitions while the mic was busy; want 0", got)
	}

	hub.ReleaseAudio("phone")
	for _, frame := range voicedFrames(20) {
		source.frames <- frame
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(950 * time.Millisecond)
	source.frames <- voicedFrames(1)[0]
	time.Sleep(200 * time.Millisecond)

	if got := engine.count(); got == 0 {
		t.Fatal("wake checks did not resume after the mic was released")
	}
}

// Slow recognitions must not stack: a local engine cold-starts per call, so
// unbounded concurrent checks multiply processes and cloud bills.
func TestWakeRunsOneRecognitionAtATime(t *testing.T) {
	engine := &countingEngine{started: make(chan struct{}, 8), gate: make(chan struct{})}
	_, source := startWakeLoop(t, engine)

	for _, frame := range voicedFrames(20) {
		source.frames <- frame
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case <-engine.started:
	case <-time.After(3 * time.Second):
		t.Fatal("first wake recognition never started")
	}

	// Two more throttle windows pass while the first call is still held.
	for i := 0; i < 24; i++ {
		source.frames <- voicedFrames(1)[0]
		time.Sleep(100 * time.Millisecond)
	}
	if got := engine.count(); got != 1 {
		t.Fatalf("%d recognitions ran concurrently; want 1 in flight", got)
	}

	close(engine.gate)
	time.Sleep(200 * time.Millisecond)
	source.frames <- voicedFrames(1)[0]
	time.Sleep(1100 * time.Millisecond)
	source.frames <- voicedFrames(1)[0]
	time.Sleep(300 * time.Millisecond)
	if got := engine.count(); got < 2 {
		t.Fatalf("checks did not resume after the first finished (calls=%d)", got)
	}
}
