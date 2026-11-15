//go:build linux

package paste

// The X11 paste path needs a real display for its XTEST half, so what a unit
// can pin is the routing and the clipboard hand-off (OPS-13): wtype wins on
// Wayland, xsel carries the text on the fallback, and a missing xsel fails
// with an install hint instead of a bare exec error.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeStub(t *testing.T, dir, name, script string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatalf("write stub %s: %v", name, err)
	}
}

func TestInjectX11WithoutXselFailsWithInstallHint(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	err := injectX11(context.Background(), "hello")
	if err == nil || !strings.Contains(err.Error(), "xsel not installed") {
		t.Fatalf("err = %v, want the xsel install hint", err)
	}
}

func TestInjectPrefersWtypeOnWayland(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "wtype.log")
	writeStub(t, dir, "wtype", "#!/bin/sh\n/bin/cat > \"$STUB_LOG\"\n")
	t.Setenv("STUB_LOG", log)
	t.Setenv("PATH", dir)
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("DISPLAY", "")

	if err := injectBackend(context.Background(), "hello wtype", BackendAuto); err != nil {
		t.Fatalf("inject: %v", err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("wtype never ran: %v", err)
	}
	if string(data) != "hello wtype" {
		t.Fatalf("wtype stdin = %q, want %q", data, "hello wtype")
	}
}

func TestInjectFallsBackToX11ClipboardPaste(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "xsel.log")
	writeStub(t, dir, "xsel", "#!/bin/sh\n/bin/cat > \"$STUB_LOG\"\n")
	t.Setenv("STUB_LOG", log)
	t.Setenv("PATH", dir)
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", "")

	err := injectBackend(context.Background(), "hello x11", BackendAuto)
	// xsel carries the clipboard first; without an X server the XTEST half
	// fails after it — proving the fallback path ran, text and all.
	if err == nil || !strings.Contains(err.Error(), "connect X display") {
		t.Fatalf("err = %v, want the X display failure after clipboard set", err)
	}
	data, readErr := os.ReadFile(log)
	if readErr != nil {
		t.Fatalf("xsel never ran: %v", readErr)
	}
	if string(data) != "hello x11" {
		t.Fatalf("xsel stdin = %q, want %q", data, "hello x11")
	}
}
