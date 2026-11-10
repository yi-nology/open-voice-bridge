package asr

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	oeerrors "github.com/oekit/pkg/errors"
)

// guardBinary enforces SEC-06 before a configured ASR binary is executed.
// The path must resolve to a regular file that is executable and not writable
// by group or others, owned by root or the running user. This keeps a tampered
// config from pointing at a binary another local user controls: without it,
// asr.command.binary turns "reach the admin API" into "run anything".
func guardBinary(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return oeerrors.Errorf("binary is required")
	}
	resolved := path
	if !filepath.IsAbs(resolved) {
		looked, err := exec.LookPath(resolved)
		if err != nil {
			return oeerrors.Errorf("binary %q was not found in PATH", path)
		}
		resolved = looked
	}
	resolved, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		return oeerrors.Errorf("binary %q cannot be resolved: %w", path, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return oeerrors.Errorf("binary %q is not usable: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return oeerrors.Errorf("binary %q is not a regular file", path)
	}
	perm := info.Mode().Perm()
	if perm&0o111 == 0 {
		return oeerrors.Errorf("binary %q is not executable (mode %04o)", path, perm)
	}
	if perm&0o022 != 0 {
		return oeerrors.Errorf("binary %q is writable by group or others (mode %04o)", path, perm)
	}
	if uid, ok := fileOwnerUID(info); ok && uid != 0 && uid != os.Geteuid() {
		return oeerrors.Errorf("binary %q is owned by another user", path)
	}
	return nil
}
