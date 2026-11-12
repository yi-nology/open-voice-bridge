//go:build linux

package paste

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

// sendKey prefers the Wayland virtual keyboard (wtype) and falls back to
// xdotool, mirroring how inject picks its transport.
func sendKey(ctx context.Context, combo string) error {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		if _, err := exec.LookPath("wtype"); err == nil {
			args := wtypeCombo(combo)
			if args == nil {
				return ErrUnsupported
			}
			return exec.CommandContext(ctx, "wtype", args...).Run()
		}
	}
	return exec.CommandContext(ctx, "xdotool", "key", "--clearmodifiers", combo).Run()
}

// wtypeCombo splits "ctrl+shift+c" into wtype's press/type/release flags.
func wtypeCombo(combo string) []string {
	parts := strings.Split(strings.ToLower(combo), "+")
	key := parts[len(parts)-1]
	args := []string{}
	for _, mod := range parts[:len(parts)-1] {
		name, ok := wtypeMods[mod]
		if !ok {
			return nil
		}
		args = append(args, "-M", name)
	}
	args = append(args, "-k", wtypeKey(key))
	for _, mod := range parts[:len(parts)-1] {
		args = append(args, "-m", wtypeMods[mod])
	}
	return args
}

var wtypeMods = map[string]string{
	"ctrl": "ctrl", "shift": "shift", "alt": "alt",
	"super": "logo", "meta": "logo", "win": "logo",
}

// wtypeKey maps the friendly names voice commands use onto X keysyms.
var wtypeKeys = map[string]string{
	"enter": "Return", "return": "Return", "backspace": "BackSpace",
	"tab": "Tab", "esc": "Escape", "escape": "Escape", "space": "space",
	"delete": "Delete", "home": "Home", "end": "End",
	"pageup": "Page_Up", "pagedown": "Page_Down",
	"up": "Up", "down": "Down", "left": "Left", "right": "Right",
	"f1": "F1", "f2": "F2", "f3": "F3", "f4": "F4", "f5": "F5", "f6": "F6",
	"f7": "F7", "f8": "F8", "f9": "F9", "f10": "F10", "f11": "F11", "f12": "F12",
}

func wtypeKey(key string) string {
	if name, ok := wtypeKeys[key]; ok {
		return name
	}
	return key
}
