//go:build darwin

package paste

import (
	"context"
	"errors"
	"os/exec"
	"strings"
)

// inject puts text on the pasteboard and synthesizes ⌘V. Both steps are plain
// child processes (no cgo), matching the Linux wtype/xsel shape. The host
// needs Accessibility permission for the keystroke half ("osascript is not
// allowed to send keystrokes" until it is granted); without it the
// transcription still lands in subtitles and history.
func inject(ctx context.Context, text string) error {
	copyCmd := exec.CommandContext(ctx, "pbcopy")
	copyCmd.Stdin = strings.NewReader(text)
	if err := copyCmd.Run(); err != nil {
		return err
	}
	pasteCmd := exec.CommandContext(ctx, "osascript", "-e",
		`tell application "System Events" to keystroke "v" using command down`)
	if output, err := pasteCmd.CombinedOutput(); err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return err
		}
		return errors.New(message)
	}
	return nil
}
