//go:build linux

package paste

import (
	"context"
	"os"
	"os/exec"
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
