//go:build linux

package paste

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

// injectBackend types text through the named backend. auto prefers the native
// Wayland virtual-keyboard protocol when wtype is available and falls back to
// X11/XWayland; the explicit backends skip the guess.
func injectBackend(ctx context.Context, text, backend string) error {
	if backend != BackendX11 && os.Getenv("WAYLAND_DISPLAY") != "" {
		if _, err := exec.LookPath("wtype"); err == nil {
			command := exec.CommandContext(ctx, "wtype", "-")
			command.Stdin = strings.NewReader(text)
			err := command.Run()
			// auto falls back to X11 when wtype fails; a forced wayland run
			// reports the failure instead of silently using another path.
			if err == nil || backend == BackendWayland {
				return err
			}
		}
	}
	return injectX11(ctx, text)
}
