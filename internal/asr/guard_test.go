package asr

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// SEC-06: asr.command.binary is executed verbatim. A tampered config must not
// be able to point it at a binary another local user can swap in.
func TestCommandBinaryGuardRejectsUntrustedPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file modes")
	}
	dir := t.TempDir()

	mustWrite := func(name string, mode os.FileMode) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\necho ok\n"), mode); err != nil {
			t.Fatal(err)
		}
		// WriteFile applies the umask; force the intended mode.
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		return path
	}
	missing := filepath.Join(dir, "does-not-exist")

	cases := []struct {
		name string
		path string
	}{
		{"world writable", mustWrite("world.sh", 0o777)},
		{"group writable", mustWrite("group.sh", 0o775)},
		{"not executable", mustWrite("plain.sh", 0o600)},
		{"missing", missing},
		{"directory", dir},
		{"relative name", "definitely-not-on-path-xyz"},
		{"empty", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewCommand(CommandConfig{Binary: tc.path}); err == nil {
				t.Fatalf("binary %q accepted, want guard rejection", tc.path)
			}
		})
	}
}

func TestCommandBinaryGuardAllowsOwnedExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file modes")
	}
	script := filepath.Join(t.TempDir(), "ok.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf ok\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCommand(CommandConfig{Binary: script}); err != nil {
		t.Fatalf("owned executable rejected: %v", err)
	}
}

// The whisper.cpp engine runs its binary the same way, so it takes the same
// guard.
func TestWhisperBinaryGuardRejectsWritableBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file modes")
	}
	script := filepath.Join(t.TempDir(), "whisper.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(script, 0o777); err != nil {
		t.Fatal(err)
	}
	model := filepath.Join(t.TempDir(), "model.bin")
	if err := os.WriteFile(model, []byte("fake"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewWhisperCPP(WhisperCPPConfig{Binary: script, ModelPath: model}); err == nil {
		t.Fatal("world-writable whisper binary accepted, want guard rejection")
	}
}
