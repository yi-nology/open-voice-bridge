package agent

import (
	"testing"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/input"
)

func TestRemoteSourceStatus(t *testing.T) {
	agent := &Agent{
		cfg:       config.Config{Address: "AA:BB:CC:DD:EE:FF"},
		connected: true,
		address:   "AA:BB:CC:DD:EE:FF",
		lastAt:    time.Unix(1700000000, 0),
	}
	source := remoteSource{agent}
	if source.ID() != "remote" || source.Kind() != input.KindRemote {
		t.Fatalf("id/kind = %q/%q", source.ID(), source.Kind())
	}
	status := source.Status()
	if status.Address != "AA:BB:CC:DD:EE:FF" || !status.Connected {
		t.Fatalf("status = %+v", status)
	}
	if status.Speaking {
		t.Fatal("expected idle remote")
	}
	if !status.LastActive.Equal(time.Unix(1700000000, 0)) {
		t.Fatalf("last_active = %v", status.LastActive)
	}

	agent.dictation = []int16{1, 2, 3}
	if !source.Status().Speaking {
		t.Fatal("expected speaking remote while capturing dictation")
	}
}

func TestSetHubRegistersRemote(t *testing.T) {
	agent := &Agent{cfg: config.Config{Address: "AA:BB:CC:DD:EE:FF"}, address: "AA:BB:CC:DD:EE:FF"}
	hub := input.NewHub()
	agent.SetHub(hub)
	statuses := hub.Sources()
	if len(statuses) != 1 || statuses[0].ID != "remote" {
		t.Fatalf("hub sources = %+v", statuses)
	}
	// Setting a nil hub must not panic and leaves the agent usable.
	agent.SetHub(nil)
}
