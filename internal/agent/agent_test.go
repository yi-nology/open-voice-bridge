package agent

import (
	"context"
	"github.com/yi-nology/open-voice-bridge/internal/speech"
	"testing"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/input"
)

// TestRecordTranscriptNeverBlocksOnAFullStream (P2-1): commands and partials
// used to ride a bare channel send while dictation used the non-blocking
// publish — a full subtitle stream (or a reader that exited) hung the
// pipeline sink and, with it, the paste path. Everything drops instead.
func TestRecordTranscriptNeverBlocksOnAFullStream(t *testing.T) {
	a := New(config.Config{MaxUtterance: 30}, &fakeEngine{}, fakeSink{healthy: true})
	for i := 0; i < cap(a.transcripts); i++ {
		a.transcripts <- speech.Transcript{Text: "filler"}
	}
	done := make(chan struct{})
	go func() {
		a.recordTranscript(speech.Transcript{Text: "复制", Command: "复制"})
		a.recordTranscript(speech.Transcript{Text: "片段", Partial: true})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("recordTranscript blocked on a full transcripts stream")
	}
}

func TestPublishTranscript(t *testing.T) {
	agent := &Agent{transcripts: make(chan speech.Transcript, 1)}
	at := time.Now()
	agent.publishTranscript(speech.Transcript{Text: "你好", Provider: "fake", At: at})
	select {
	case got := <-agent.Transcripts():
		if got.Text != "你好" || got.Provider != "fake" || !got.At.Equal(at) {
			t.Fatalf("transcript = %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("transcript was not published")
	}
}

type fakeSink struct{ healthy bool }

func (f fakeSink) Start(context.Context) error { return nil }
func (f fakeSink) Write([]int16) error         { return nil }
func (f fakeSink) Close(context.Context) error { return nil }
func (f fakeSink) Name() string                { return "fake" }
func (f fakeSink) Healthy() bool               { return f.healthy }

func TestStatusReportsMicHealthAndLatency(t *testing.T) {
	agent := &Agent{audio: fakeSink{healthy: true}, lastLatency: 320 * time.Millisecond}
	status := agent.Status()
	if !status.MicHealthy {
		t.Fatal("expected healthy mic")
	}
	if status.LastLatency != 320*time.Millisecond {
		t.Fatalf("last latency = %v", status.LastLatency)
	}
	agent.audio = fakeSink{healthy: false}
	if agent.Status().MicHealthy {
		t.Fatal("expected unhealthy mic")
	}
}

// DOM-14: SetHub may race readers (acquireMic/releaseMic run on the audio
// path). Meaningful under -race.
func TestSetHubConcurrentWithMicHooks(t *testing.T) {
	a := &Agent{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			a.SetHub(input.NewHub())
		}
	}()
	for i := 0; i < 200; i++ {
		a.acquireMic()
		a.releaseMic()
	}
	<-done
}
