package agent

import (
	"context"
	"testing"
	"time"
)

func TestPublishTranscript(t *testing.T) {
	agent := &Agent{transcripts: make(chan Transcript, 1)}
	at := time.Now()
	agent.publishTranscript(Transcript{Text: "你好", Provider: "fake", At: at})
	select {
	case got := <-agent.Transcripts():
		if got.Text != "你好" || got.Provider != "fake" || !got.At.Equal(at) {
			t.Fatalf("transcript = %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("transcript was not published")
	}
}

func TestPublishListening(t *testing.T) {
	agent := &Agent{listening: make(chan bool, 1)}
	agent.publishListening(true)
	select {
	case active := <-agent.Listening():
		if !active {
			t.Fatal("expected listening")
		}
	case <-time.After(time.Second):
		t.Fatal("listening not published")
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
	if status["mic_healthy"] != true {
		t.Fatalf("mic_healthy = %v", status["mic_healthy"])
	}
	if status["last_latency_ms"] != int64(320) {
		t.Fatalf("last_latency_ms = %v", status["last_latency_ms"])
	}
	agent.audio = fakeSink{healthy: false}
	if agent.Status()["mic_healthy"] != false {
		t.Fatal("expected unhealthy mic")
	}
}
