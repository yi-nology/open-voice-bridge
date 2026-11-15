package ui

import "testing"

func eventNames(t *testing.T, frames []any) []string {
	t.Helper()
	var seen []string
	for _, event := range frames {
		switch v := event.(type) {
		case map[string]any:
			seen = append(seen, v["event"].(string))
		case sseGap:
			seen = append(seen, v.Event)
		default:
			t.Fatalf("event = %+v, want map or sseGap", event)
		}
	}
	return seen
}

func TestPublishBroadcasts(t *testing.T) {
	server := New(Deps{})
	client := &sseClient{ch: make(chan any, 1)}
	server.clients[client] = struct{}{}
	server.Publish(map[string]any{"event": "transcript", "text": "你好"})
	select {
	case event := <-client.ch:
		fields, ok := event.(map[string]any)
		if !ok || fields["event"] != "transcript" || fields["text"] != "你好" {
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

	server.Publish(map[string]any{"event": "newest", "text": "你好"})

	var frames []any
	for len(client.ch) > 0 {
		frames = append(frames, <-client.ch)
	}
	seen := eventNames(t, frames)
	if len(seen) != 2 || seen[0] != "newest" || seen[1] != "stream_gap" {
		t.Fatalf("buffer after overflow = %v, want [newest stream_gap]", seen)
	}
	if client.dropped == 0 {
		t.Fatal("drops must be counted (I4)")
	}
}
