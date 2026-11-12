//go:build linux

package keys

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oekit/logx"
	"golang.org/x/sys/unix"
)

// captureLogs redirects the default logger for one test: keys logs through
// logx.Delegating, which resolves the default per call.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := logx.Default()
	logx.SetDefault(logx.NewSlogLogger("keys-test", logx.WithHandler(slog.NewTextHandler(&buf, nil))))
	t.Cleanup(func() { logx.SetDefault(previous) })
	return &buf
}

// unreadable creates a node the watcher cannot open. Root bypasses the mode
// bits, so the EACCES these tests assert cannot be produced there.
func unreadable(t *testing.T) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	path := filepath.Join(t.TempDir(), "event9")
	if err := os.WriteFile(path, nil, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	return path
}

func chmod(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

// OPS-05: the watcher retried every 2s without a word, so a user whose account
// cannot read evdev saw a dead voice key and an empty journal. The first
// failure must say what to do about it, and the retry must stay quiet.
func TestOpenFailureIsReportedOnceWithAFix(t *testing.T) {
	sink := captureLogs(t)
	path := unreadable(t)
	watcher := newF24Watcher().(*f24Watcher)
	for attempt := 0; attempt < 3; attempt++ {
		if _, err := watcher.openEventNode(path); err == nil {
			t.Fatalf("attempt %d: opened an unreadable node", attempt)
		}
	}
	report := sink.String()
	if lines := strings.Count(report, "\n"); lines != 1 {
		t.Fatalf("got %d log lines for 3 failures, want 1:\n%s", lines, report)
	}
	for _, want := range []string{"usermod -aG input", path, "voice key"} {
		if !strings.Contains(report, want) {
			t.Fatalf("report missing %q:\n%s", want, report)
		}
	}
}

// A fix has to be visible: once the node opens, a later outage is a new event
// and must be reported again rather than suppressed forever.
func TestOpenFailureIsReportedAgainAfterRecovery(t *testing.T) {
	sink := captureLogs(t)
	path := unreadable(t)
	watcher := newF24Watcher().(*f24Watcher)
	if _, err := watcher.openEventNode(path); err == nil {
		t.Fatal("opened an unreadable node")
	}
	chmod(t, path, 0o600)
	fd, err := watcher.openEventNode(path)
	if err != nil {
		t.Fatalf("readable node: %v", err)
	}
	if err := unix.Close(fd); err != nil {
		t.Fatalf("close: %v", err)
	}
	chmod(t, path, 0o000)
	if _, err := watcher.openEventNode(path); err == nil {
		t.Fatal("opened an unreadable node again")
	}
	if lines := strings.Count(sink.String(), "\n"); lines != 2 {
		t.Fatalf("got %d log lines across two outages, want 2:\n%s", lines, sink.String())
	}
}

// A node that is not there is a different problem: telling the user to join
// the input group would send them down the wrong path.
func TestMissingNodeIsReportedWithoutGroupAdvice(t *testing.T) {
	sink := captureLogs(t)
	path := filepath.Join(t.TempDir(), "absent-event")
	watcher := newF24Watcher().(*f24Watcher)
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := watcher.openEventNode(path); err == nil {
			t.Fatalf("attempt %d: opened a missing node", attempt)
		}
	}
	report := sink.String()
	if lines := strings.Count(report, "\n"); lines != 1 {
		t.Fatalf("got %d log lines for 2 failures, want 1:\n%s", lines, report)
	}
	if !strings.Contains(report, path) {
		t.Fatalf("report does not name the node:\n%s", report)
	}
	if strings.Contains(report, "usermod") {
		t.Fatalf("a missing node must not advise a group change:\n%s", report)
	}
}
