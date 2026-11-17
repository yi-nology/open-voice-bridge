package ui

import (
	"encoding/json"
	"testing"

	"github.com/yi-nology/open-voice-bridge/internal/core"
)

// eventNames reads each frame's wire "event" key, whatever envelope type
// carried it (typed events, the gap marker, or a raw map in a buffer preload).
func eventNames(t *testing.T, frames []any) []string {
	t.Helper()
	var seen []string
	for _, event := range frames {
		payload, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}
		var wire struct {
			Event string `json:"event"`
		}
		if err := json.Unmarshal(payload, &wire); err != nil {
			t.Fatalf("unmarshal event: %v", err)
		}
		seen = append(seen, wire.Event)
	}
	return seen
}

func TestPublishBroadcasts(t *testing.T) {
	server := New(Deps{})
	client := &sseClient{ch: make(chan any, 1)}
	server.clients[client] = struct{}{}
	server.Publish(core.TranscriptFinalEvent{Event: "transcript", Text: "你好"})
	select {
	case event := <-client.ch:
		fields, ok := event.(core.TranscriptFinalEvent)
		if !ok || fields.Event != "transcript" || fields.Text != "你好" {
			t.Fatalf("event = %+v", event)
		}
	default:
		t.Fatal("event was not broadcast")
	}
}

// I4: a full client buffer drops the oldest event, not the newest, and the
// drop is visible (stream_gap) instead of silent.
func TestBroadcastDropsOldestAndFlagsGap(t *testing.T) {
	server := New(Deps{})
	client := &sseClient{ch: make(chan any, 2)}
	client.ch <- map[string]any{"event": "old-1"}
	client.ch <- map[string]any{"event": "old-2"}
	server.clients[client] = struct{}{}

	server.Publish(core.TranscriptFinalEvent{Event: "transcript", Text: "你好"})

	var frames []any
	for len(client.ch) > 0 {
		frames = append(frames, <-client.ch)
	}
	seen := eventNames(t, frames)
	if len(seen) != 2 || seen[0] != "transcript" || seen[1] != "stream_gap" {
		t.Fatalf("buffer after overflow = %v, want [transcript stream_gap]", seen)
	}
	if client.dropped == 0 {
		t.Fatal("drops must be counted (I4)")
	}
}
