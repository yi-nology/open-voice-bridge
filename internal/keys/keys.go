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
	// AgentActionBase..+AgentActionSlots (F25…) hand deferred seq steps —
	// type()/open() — to the user-level agent (S3).
	AgentActionBase  = 195
	AgentActionSlots = 12
)

// SlotForCode maps a synthetic key code to a phrase slot (1-based).
func SlotForCode(code int) (int, bool) {
	if code < PhraseKeyBase || code >= PhraseKeyBase+PhraseSlots {
		return 0, false
	}
	return code - PhraseKeyBase + 1, true
}

// AgentSlotForCode reports which deferred action slot a synthetic key names.
func AgentSlotForCode(code int) (int, bool) {
	if code < AgentActionBase || code >= AgentActionBase+AgentActionSlots {
		return 0, false
	}
	return code - AgentActionBase, true
}

// F24Watcher emits a value on C for every F24 key-down (voice), a slot
// number on Phrases for every F13–F20 key-down (common phrases), a
// value on Picker for F21 (open the phrase-picker overlay), and a slot
// number on Actions for every F25+ key-down (deferred seq steps).
type F24Watcher interface {
	Start(ctx context.Context) error
	Close(ctx context.Context) error
	C() <-chan struct{}
	Phrases() <-chan int
	Picker() <-chan struct{}
	Actions() <-chan int
}

func NewF24Watcher() F24Watcher { return newF24Watcher() }
