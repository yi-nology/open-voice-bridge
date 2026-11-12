// Package keys watches for the agent-facing synthetic keys the root remapper
// emits: F24 opens the microphone, F13–F20 insert a common phrase.
package keys

import "context"

// PhraseSlot codes are injected by the remap daemon for the phrase1..phrase8
// mapping actions; the agent turns a key-down into "insert phrase n".
const (
	PhraseKeyBase = 183 // F13: slot 1
	PhraseSlots   = 8   // F13–F20
	PickerKey     = 191 // F21: open the phrase-picker overlay
)

// SlotForCode maps a synthetic key code to a phrase slot (1-based).
func SlotForCode(code int) (int, bool) {
	if code < PhraseKeyBase || code >= PhraseKeyBase+PhraseSlots {
		return 0, false
	}
	return code - PhraseKeyBase + 1, true
}

// F24Watcher emits a value on C for every F24 key-down (voice), a slot
// number on Phrases for every F13–F20 key-down (common phrases), and a
// value on Picker for F21 (open the phrase-picker overlay).
type F24Watcher interface {
	Start(ctx context.Context) error
	Close(ctx context.Context) error
	C() <-chan struct{}
	Phrases() <-chan int
	Picker() <-chan struct{}
}

func NewF24Watcher() F24Watcher { return newF24Watcher() }
