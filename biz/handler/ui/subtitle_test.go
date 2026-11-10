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

// I4: a full client buffer drops the oldest event, not the newest, and the
// drop is visible (stream_gap) instead of silent.
func TestBroadcastDropsOldestAndFlagsGap(t *testing.T) {
	server := New(Deps{})
	client := &sseClient{ch: make(chan map[string]any, 2)}
	client.ch <- map[string]any{"event": "old-1"}
	client.ch <- map[string]any{"event": "old-2"}
	server.clients[client] = struct{}{}

	server.Publish(map[string]any{"event": "newest", "text": "你好"})

	var seen []string
	for len(client.ch) > 0 {
		event := <-client.ch
		seen = append(seen, event["event"].(string))
	}
	if len(seen) != 2 || seen[0] != "newest" || seen[1] != "stream_gap" {
		t.Fatalf("buffer after overflow = %v, want [newest stream_gap]", seen)
	}
	if client.dropped == 0 {
		t.Fatal("drops must be counted (I4)")
	}
}

// I4: every new subscriber receives a state snapshot first, so a page that
// missed a listening/capture event can resync instead of staying desynced.
func TestRegisterClientReturnsSnapshot(t *testing.T) {
	server := New(Deps{
		SnapshotState: func() map[string]any {
			return map[string]any{"listening": map[string]any{"active": true, "source": "phone", "source_name": "客厅手机"}}
		},
	})
	client := &sseClient{ch: make(chan map[string]any, 1)}
	snap := server.registerClient(client)
	if snap["event"] != "snapshot" {
		t.Fatalf("snapshot event = %v", snap["event"])
	}
	if snap["capturing"] != false {
		t.Fatalf("capturing = %v", snap["capturing"])
	}
	listening, ok := snap["listening"].(map[string]any)
	if !ok || listening["active"] != true || listening["source"] != "phone" {
		t.Fatalf("listening = %+v", snap["listening"])
	}
}
