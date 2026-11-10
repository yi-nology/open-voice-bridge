package phone

import (
	"sync"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/input"
)

// Source is the hub-facing view of one connected phone. The server creates a
// Source per live WebSocket, registers it with the InputHub so the desktop can
// show "who is connected", and unregisters it on disconnect.
type Source struct {
	// id, name and address are fixed at construction and never rewritten, so
	// they are read without the mutex.
	id        string
	name      string
	address   string
	mu        sync.Mutex
	connected bool
	speaking  bool
	lastSeen  time.Time
	onPreempt func() // set by the server; delivers the partial + notice (I1)
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

func (s *Source) Name() string    { return s.name }
func (s *Source) Address() string { return s.address }

// isSpeaking reports whether a hold-to-talk utterance is in flight, which is
// what makes stray binary frames outside a session ignorable.
func (s *Source) isSpeaking() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.speaking
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

// Preempt stops a hold-to-talk capture because a stronger source claimed the
// microphone (I1). The flag flips synchronously so stray frames stop landing;
// the server's hook then delivers what was spoken and notifies the phone.
func (s *Source) Preempt() {
	s.mu.Lock()
	wasSpeaking := s.speaking
	s.speaking = false
	hook := s.onPreempt
	s.mu.Unlock()
	if wasSpeaking && hook != nil {
		hook()
	}
}
