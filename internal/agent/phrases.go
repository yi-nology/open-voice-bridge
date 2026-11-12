package agent

import (
	"context"
	"time"

	"github.com/oekit/concurrency/async"

	"github.com/yi-nology/open-voice-bridge/internal/keys"
)

// Watcher exposes the agent's synthetic-key watcher so companion components
// (the phrase inserter) share one evdev reader instead of opening a second.
func (a *Agent) Watcher() keys.F24Watcher { return a.watcher }

// PhraseEvent is one common phrase inserted by a phrase1..phrase8 key action.
// It is user-authored text, not speech: it goes to the live UI only and never
// enters history or the log body.
type PhraseEvent struct {
	Slot int
	Text string
	At   time.Time
}

// PhraseWatcher turns the synthetic F13–F20 key-downs the root remapper emits
// for phrase1..phrase8 into clipboard insertions on the shared speech
// pipeline. It lives outside Agent so a keymap rebind and a dictation can
// proceed independently.
// PickerEvent asks the live UIs to open the phrase-picker overlay (F21).
type PickerEvent struct {
	At time.Time
}

type PhraseWatcher struct {
	watcher  keys.F24Watcher
	pipeline *SpeechPipeline
	events   chan PhraseEvent
	pickers  chan PickerEvent
}

func NewPhraseWatcher(watcher keys.F24Watcher, pipeline *SpeechPipeline) *PhraseWatcher {
	return &PhraseWatcher{
		watcher:  watcher,
		pipeline: pipeline,
		events:   make(chan PhraseEvent, 8),
		pickers:  make(chan PickerEvent, 1),
	}
}

// Events yields insertion notices for the live UI.
func (w *PhraseWatcher) Events() <-chan PhraseEvent { return w.events }

// Pickers yields open-the-overlay requests for the live UI.
func (w *PhraseWatcher) Pickers() <-chan PickerEvent { return w.pickers }

// Run consumes phrase key-downs until ctx ends. Key-downs that name an empty
// slot stay silent so a stale keymap is a no-op, never a wrong paste.
func (w *PhraseWatcher) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.watcher.Picker():
			select {
			case w.pickers <- PickerEvent{At: time.Now()}:
			default:
			}
		case slot := <-w.watcher.Phrases():
			text, ok := w.pipeline.InsertPhrase(slot)
			if !ok {
				log.Debug("phrase slot empty", "slot", slot)
				continue
			}
			// The phrase body is user content and stays out of the log.
			log.Info("phrase inserted", "slot", slot, "chars", len([]rune(text)))
			select {
			case w.events <- PhraseEvent{Slot: slot, Text: text, At: time.Now()}:
			default:
				log.Debug("phrase event stream full; dropping notice")
			}
		}
	}
}

// Start runs the watcher on its own goroutine for callers that just want it
// running alongside the agent.
func (w *PhraseWatcher) Start(ctx context.Context) { async.GoSafe(func() { w.Run(ctx) }) }
