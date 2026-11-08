//go:build linux

package paste

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

// inject prefers the native Wayland virtual-keyboard protocol when wtype is
// available, and falls back to X11/XWayland clipboard paste otherwise.
func inject(ctx context.Context, text string) error {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		if _, err := exec.LookPath("wtype"); err == nil {
			command := exec.CommandContext(ctx, "wtype", "-")
			command.Stdin = strings.NewReader(text)
			return command.Run()
		}
	}
	return injectX11(ctx, text)
}
