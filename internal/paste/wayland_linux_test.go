//go:build linux

package paste

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWaylandInjectUsesWtype(t *testing.T) {
	dir := t.TempDir()
	capture := filepath.Join(dir, "captured.txt")
	script := filepath.Join(dir, "wtype")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat > \"$CAPTURE\"\n"), 0o755); err != nil {
		t.Fatalf("write fake wtype: %v", err)
	}
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("CAPTURE", capture)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := Text(context.Background(), "你好 world"); err != nil {
		t.Fatalf("Text: %v", err)
	}
	got, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	if string(got) != "你好 world" {
		t.Fatalf("captured = %q, want %q", got, "你好 world")
	}
}
