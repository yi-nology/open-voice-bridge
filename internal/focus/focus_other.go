//go:build !linux

package focus

import "errors"

// ActiveClass is X11-only; other platforms have no focus query and the
// per-app keymap switcher stays idle.
func ActiveClass() (string, error) {
	return "", errors.New("focus query is not supported on this platform")
}
