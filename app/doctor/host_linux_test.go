//go:build linux

package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// unreadableNode creates a node the current user cannot open, which is what
// /dev/input/eventN looks like to a user outside the input group.
func unreadableNode(t *testing.T) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root reads mode-000 nodes, so the permission gap cannot be reproduced")
	}
	path := filepath.Join(t.TempDir(), "event99")
	if err := os.WriteFile(path, nil, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	return path
}

// OPS-05's other half: presence is not permission. The watcher retries EACCES
// every 2s, so doctor must say the node is unreadable and name the group fix
// instead of reporting the remote as fine.
func TestEvdevReadCheckFlagsAnUnreadableNode(t *testing.T) {
	path := unreadableNode(t)
	checks := evdevReadChecks([]string{path})
	if len(checks) != 1 {
		t.Fatalf("checks = %+v, want one", checks)
	}
	line := renderChecks(t, checks)
	if !strings.HasPrefix(line, "WARN:") {
		t.Fatalf("line = %q, want a WARN: the remote is optional", line)
	}
	for _, want := range []string{path, "usermod -aG input"} {
		if !strings.Contains(line, want) {
			t.Errorf("line missing %q: %q", want, line)
		}
	}
}

func TestEvdevReadCheckAcceptsAReadableNode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "event99")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	line := renderChecks(t, evdevReadChecks([]string{path}))
	if !strings.HasPrefix(line, "ok:") || !strings.Contains(line, path) {
		t.Fatalf("line = %q, want ok naming the node", line)
	}
}

// A node that vanished between discovery and the probe is not a group problem,
// so it must not send the user off to usermod.
func TestEvdevReadCheckDoesNotBlameTheGroupForAMissingNode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent-event99")
	line := renderChecks(t, evdevReadChecks([]string{path}))
	if !strings.HasPrefix(line, "WARN:") {
		t.Fatalf("line = %q, want a WARN", line)
	}
	if strings.Contains(line, "usermod") {
		t.Fatalf("a missing node must not blame the input group: %q", line)
	}
}

func renderChecks(t *testing.T, checks []check) string {
	t.Helper()
	var out strings.Builder
	if n := report(&out, checks); n != 0 {
		t.Fatalf("failures = %d, want 0: the remote is optional\n%s", n, out.String())
	}
	return strings.TrimSpace(out.String())
}
