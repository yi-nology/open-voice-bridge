package phone

import (
	"sync"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/input"
)

// Source is the hub-facing view of one connected phone. The server creates a
// Source per live WebSocket, registers it with the InputHub so the desktop can
// show "who is connected", and unregisters it on disconnect. Speaking stays
// false in Phase 1 (keys only); Phase 2 flips it while a mic utterance streams.
type Source struct {
	mu        sync.Mutex
	id        string
	name      string
	address   string
	connected bool
	speaking  bool
	lastSeen  time.Time
}

func newSource(deviceID, name, address string) *Source {
	return &Source{id: deviceID, name: name, address: address, connected: true, lastSeen: time.Now()}
}

func (s *Source) ID() string       { return s.id }
func (s *Source) Kind() input.Kind { return input.KindPhone }

func (s *Source) Status() input.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return input.Status{
		ID:         s.id,
		Kind:       input.KindPhone,
		Name:       s.name,
		Address:    s.address,
		Connected:  s.connected,
		Speaking:   s.speaking,
		LastActive: s.lastSeen,
	}
}

func (s *Source) touch() {
	s.mu.Lock()
	s.lastSeen = time.Now()
	s.mu.Unlock()
}

func (s *Source) setSpeaking(active bool) {
	s.mu.Lock()
	s.speaking = active
	if active {
		s.lastSeen = time.Now()
	}
	s.mu.Unlock()
}

func (s *Source) disconnect() {
	s.mu.Lock()
	s.connected = false
	s.speaking = false
	s.mu.Unlock()
}
