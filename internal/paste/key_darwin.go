//go:build darwin

package paste

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// sendKey drives System Events: single characters go through keystroke,
// named keys through key codes; ctrl is macOS Command.
func sendKey(ctx context.Context, combo string) error {
	parts := strings.Split(strings.ToLower(combo), "+")
	key := parts[len(parts)-1]
	mods := []string{}
	for _, mod := range parts[:len(parts)-1] {
		name, ok := macMods[mod]
		if !ok {
			return ErrUnsupported
		}
		mods = append(mods, name)
	}
	var script string
	if code, ok := macKeyCodes[key]; ok {
		script = fmt.Sprintf(`tell application "System Events" to key code %d%s`, code, macUsing(mods))
	} else if len([]rune(key)) == 1 {
		script = fmt.Sprintf(`tell application "System Events" to keystroke %q%s`, key, macUsing(mods))
	} else {
		return ErrUnsupported
	}
	return exec.CommandContext(ctx, "osascript", "-e", script).Run()
}

var macMods = map[string]string{
	"ctrl": "command down", "cmd": "command down", "super": "command down",
	"shift": "shift down", "alt": "option down", "option": "option down",
}

var macKeyCodes = map[string]int{
	"enter": 36, "return": 36, "backspace": 51, "tab": 48, "esc": 53,
	"escape": 53, "delete": 51, "space": 49, "home": 115, "end": 119,
	"up": 126, "down": 125, "left": 123, "right": 124,
	"f5": 96, "f6": 97, "f7": 98, "f8": 100, "f9": 101, "f10": 109, "f11": 103, "f12": 111,
}

func macUsing(mods []string) string {
	if len(mods) == 0 {
		return ""
	}
	return " using {" + strings.Join(mods, ", ") + "}"
}
