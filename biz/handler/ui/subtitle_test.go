package ui

import "testing"

func TestPublishBroadcasts(t *testing.T) {
	server := New(Deps{})
	client := &sseClient{ch: make(chan map[string]any, 1)}
	server.clients[client] = struct{}{}
	server.Publish(map[string]any{"event": "transcript", "text": "你好"})
	select {
	case event := <-client.ch:
		if event["event"] != "transcript" || event["text"] != "你好" {
			t.Fatalf("event = %+v", event)
		}
	default:
		t.Fatal("event was not broadcast")
	}
}
