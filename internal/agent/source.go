package agent

import (
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/input"
)

// remoteSource adapts the BLE remote to the hub's Source contract so the
// dashboard can list it alongside phone clients.
type remoteSource struct{ a *Agent }

func (remoteSource) ID() string       { return "remote" }
func (remoteSource) Kind() input.Kind { return input.KindRemote }

func (r remoteSource) Status() input.Status {
	status := r.a.Status()
	connected, _ := status["connected"].(bool)
	return input.Status{
		ID:         "remote",
		Kind:       input.KindRemote,
		Name:       r.a.cfg.DeviceName(),
		Address:    r.a.currentAddress(),
		Connected:  connected,
		Speaking:   r.a.audioActive(),
		LastActive: r.a.lastActive(),
	}
}

// lastActive returns the timestamp of the most recent finalized utterance.
func (a *Agent) lastActive() time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastAt
}
