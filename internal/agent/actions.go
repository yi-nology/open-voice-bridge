package agent

import (
	"context"
	"os/exec"
	"runtime"
	"time"

	"github.com/oekit/concurrency/async"

	"github.com/yi-nology/open-voice-bridge/internal/keys"
	"github.com/yi-nology/open-voice-bridge/internal/paste"
	"github.com/yi-nology/open-voice-bridge/internal/remap"
)

// SetSeqActions installs the deferred type()/open() slots a keymap rewrite
// produced (S3). The remapper presses agent(n) keys; this registry says what
// each slot does. It is replaced wholesale on every apply.
func (a *Agent) SetSeqActions(actions map[int]remap.AgentAction) {
	a.seqMu.Lock()
	defer a.seqMu.Unlock()
	a.seqActions = actions
}

// SeqAction reports the deferred action bound to slot n.
func (a *Agent) SeqAction(slot int) (remap.AgentAction, bool) {
	a.seqMu.Lock()
	defer a.seqMu.Unlock()
	action, ok := a.seqActions[slot]
	return action, ok
}

// SeqActionWatcher turns the synthetic agent(n) key-downs into the deferred
// work itself: type() pastes text, open() launches a program — both on the
// user side, so the root remapper never does either (S3).
type SeqActionWatcher struct {
	watcher keys.F24Watcher
	lookup  func(int) (remap.AgentAction, bool)
	// pasteText/launch are injectable for tests; defaults hit the desktop.
	pasteText func(context.Context, string) error
	launch    func(context.Context, string) error
}

func NewSeqActionWatcher(watcher keys.F24Watcher, lookup func(int) (remap.AgentAction, bool)) *SeqActionWatcher {
	return &SeqActionWatcher{
		watcher:   watcher,
		lookup:    lookup,
		pasteText: paste.Text,
		launch:    launchApp,
	}
}

// Start runs the watcher on its own goroutine.
func (w *SeqActionWatcher) Start(ctx context.Context) { async.GoSafe(func() { w.Run(ctx) }) }

// Run consumes deferred-action key-downs until ctx ends.
func (w *SeqActionWatcher) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case slot := <-w.watcher.Actions():
			action, ok := w.lookup(slot)
			if !ok {
				log.Debug("deferred seq action slot empty", "slot", slot)
				continue
			}
			w.execute(ctx, action, slot)
		}
	}
}

func (w *SeqActionWatcher) execute(ctx context.Context, action remap.AgentAction, slot int) {
	switch action.Kind {
	case "type":
		runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := w.pasteText(runCtx, action.Arg); err != nil {
			log.Warn("seq type step failed", "slot", slot, "chars", len([]rune(action.Arg)), "err", err)
			return
		}
		// The text is user content: length only, like phrases (SEC-02).
		log.Info("seq type step done", "slot", slot, "chars", len([]rune(action.Arg)))
	case "open":
		if err := w.launch(ctx, action.Arg); err != nil {
			log.Warn("seq open step failed", "slot", slot, "app", action.Arg, "err", err)
			return
		}
		log.Info("seq open step done", "slot", slot, "app", action.Arg)
	default:
		log.Debug("unknown deferred seq action", "slot", slot, "kind", action.Kind)
	}
}

// launchApp starts a program/URL the way the desktop would: Launch Services
// on macOS, xdg-open or the PATH lookup on Linux. The name comes from the
// user's own keymap config, same trust as asr.command.
func launchApp(ctx context.Context, name string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", name)
	case "windows":
		cmd = exec.CommandContext(ctx, "cmd", "/c", "start", "", name)
	default:
		if path, err := exec.LookPath(name); err == nil {
			cmd = exec.CommandContext(ctx, path)
		} else {
			cmd = exec.CommandContext(ctx, "xdg-open", name)
		}
	}
	return cmd.Run()
}
