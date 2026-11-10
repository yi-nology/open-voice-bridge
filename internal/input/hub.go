// Package input models pluggable capture sources (BLE remote, phones) and a
// hub that arbitrates which one may own the microphone at a time.
package input

import (
	"sort"
	"sync"
	"time"
)

// Kind identifies the class of an input source.
type Kind string

const (
	KindRemote Kind = "remote"
	KindPhone  Kind = "phone"
)

// RemoteID is the hub id of the BLE remote. It is exported because the agent
// claims the microphone under this id and the dashboard maps it to a label.
const RemoteID = "remote"

// kindRank orders sources for the dashboard: the BLE remote leads, then
// phones. Unknown kinds sort last by their own name.
func kindRank(kind Kind) int {
	switch kind {
	case KindRemote:
		return 0
	case KindPhone:
		return 1
	default:
		return 2
	}
}

// Status is a point-in-time snapshot of a source for the dashboard.
type Status struct {
	ID         string    `json:"id"`
	Kind       Kind      `json:"kind"`
	Name       string    `json:"name"`
	Address    string    `json:"address,omitempty"`
	Connected  bool      `json:"connected"`
	Speaking   bool      `json:"speaking"`
	LastActive time.Time `json:"last_active,omitempty"`
}

// Source is any capture device registered with the hub. Implementations must
// be safe for concurrent Status calls.
type Source interface {
	ID() string
	Kind() Kind
	Status() Status
}

// AudioState is one microphone-ownership transition.
type AudioState struct {
	Owner    string `json:"owner"`
	Speaking bool   `json:"speaking"`
}

// Hub tracks registered sources and serializes microphone ownership so only
// one source transcribes at a time; key injection is not arbitrated here.
type Hub struct {
	mu         sync.Mutex
	sources    map[string]Source
	audioOwner string
	watchers   map[chan AudioState]struct{}
}

func NewHub() *Hub {
	return &Hub{sources: map[string]Source{}, watchers: map[chan AudioState]struct{}{}}
}

func (h *Hub) Register(source Source) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sources[source.ID()] = source
}

// Unregister removes a source and releases the mic if it held it.
func (h *Hub) Unregister(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.sources, id)
	if h.audioOwner == id {
		h.audioOwner = ""
		h.notifyLocked()
	}
}

func (h *Hub) Sources() []Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Status, 0, len(h.sources))
	for _, source := range h.sources {
		out = append(out, source.Status())
	}
	sort.Slice(out, func(i, j int) bool {
		if rankI, rankJ := kindRank(out[i].Kind), kindRank(out[j].Kind); rankI != rankJ {
			return rankI < rankJ
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// AcquireAudio grants the microphone to id, or returns false when a different
// source already owns it. Re-acquiring by the owner succeeds.
func (h *Hub) AcquireAudio(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.audioOwner == "" {
		h.audioOwner = id
		h.notifyLocked()
		return true
	}
	return h.audioOwner == id
}

// AcquireAudioForce takes the microphone regardless of the current owner. The
// BLE remote uses it so dictation is never dropped because a phone started
// speaking first; RC003 audio is the primary path and always wins.
func (h *Hub) AcquireAudioForce(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.audioOwner == id {
		return
	}
	h.audioOwner = id
	h.notifyLocked()
}

// ReleaseAudio frees the microphone if id currently owns it.
func (h *Hub) ReleaseAudio(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.audioOwner == id {
		h.audioOwner = ""
		h.notifyLocked()
	}
}

// Watch returns the microphone-ownership stream. The channel is buffered with
// the current state so a new subscriber starts in sync, and later transitions
// replace that value instead of queueing behind it.
func (h *Hub) Watch() <-chan AudioState {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan AudioState, 1)
	ch <- AudioState{Owner: h.audioOwner, Speaking: h.audioOwner != ""}
	h.watchers[ch] = struct{}{}
	return ch
}

// notifyLocked fans the current ownership out to watchers. Callers hold h.mu.
// Each watcher channel has room for exactly one state, so the undrained value
// is replaced rather than queued: a stalled subscriber always converges on the
// newest state instead of being stuck behind a stale one, and a capture never
// blocks on a publisher.
func (h *Hub) notifyLocked() {
	state := AudioState{Owner: h.audioOwner, Speaking: h.audioOwner != ""}
	for ch := range h.watchers {
		select {
		case <-ch:
		default:
		}
		ch <- state
	}
}

// AudioOwner reports the source id currently holding the microphone.
func (h *Hub) AudioOwner() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.audioOwner
}
