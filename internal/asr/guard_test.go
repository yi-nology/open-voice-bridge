package asr

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// SEC-06 residual: guardBinary also rejects interpreter basenames. An
// interpreter is not a recogniser — pointing asr.command at /bin/sh (or env,
// which will happily run one) turns a tampered config into "run anything",
// and args cannot be whitelisted safely because exec.Command runs them
// verbatim. Versioned names (python3.11) count.
func TestCommandBinaryRejectsInterpreters(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file modes")
	}
	dir := t.TempDir()

	mustWrite := func(name string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf ok\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		return path
	}

	cases := []struct {
		name string
		path string
	}{
		{"sh", mustWrite("sh")},
		{"bash", mustWrite("bash")},
		{"dash", mustWrite("dash")},
		{"zsh", mustWrite("zsh")},
		{"busybox", mustWrite("busybox")},
		{"python", mustWrite("python")},
		{"python3", mustWrite("python3")},
		{"python3.11", mustWrite("python3.11")},
		{"perl", mustWrite("perl")},
		{"ruby", mustWrite("ruby")},
		{"node", mustWrite("node")},
		// env is the bottom of the barrel: it looks like an ordinary helper
		// ("env ASR_MODEL=… whisper …") and will execute any interpreter it is
		// pointed at, so it must not pass as an ASR binary either.
		{"env", mustWrite("env")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewCommand(CommandConfig{Binary: tc.path})
			if err == nil {
				t.Fatalf("interpreter binary %q accepted, want guard rejection", tc.path)
			}
			if !strings.Contains(err.Error(), "interpreter") {
				t.Fatalf("interpreter %q rejected with %q, want the error to name the reason", tc.path, err)
			}
		})
	}
}

// The blacklist must follow the symlink target, not the innocent-looking
// link name, or a link called "my-recorder" pointing at /bin/sh walks right
// through the guard.
func TestCommandBinaryRejectsInterpreterSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file modes")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "sh")
	if err := os.WriteFile(target, []byte("#!/bin/sh\nprintf ok\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "my-recorder")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCommand(CommandConfig{Binary: link}); err == nil {
		t.Fatalf("symlink %q -> %q accepted, want guard rejection", link, target)
	}
}
