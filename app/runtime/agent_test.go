package runtime

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oekit/logx"
)

func keyPermLogger(buf *bytes.Buffer) logx.Logger {
	return logx.NewSlogLogger("keyperm-test", logx.WithHandler(slog.NewTextHandler(buf, nil)))
}

func writeKey(t *testing.T, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mimo.key")
	if err := os.WriteFile(path, []byte("secret"), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

// A key other users can read is a credential leak on a shared machine, so the
// agent says so at startup — naming the path, never the key body.
func TestWarnKeyPermsFlagsAGroupReadableKey(t *testing.T) {
	path := writeKey(t, 0o644)
	var buf bytes.Buffer
	warnKeyPerms(keyPermLogger(&buf), path)
	logged := buf.String()
	if !strings.Contains(logged, "chmod 600") || !strings.Contains(logged, path) {
		t.Fatalf("log = %q, want the path and a chmod hint", logged)
	}
	if strings.Contains(logged, "secret") {
		t.Fatalf("the key body reached the log: %q", logged)
	}
}

func TestWarnKeyPermsStaysSilentForAnOwnerOnlyKey(t *testing.T) {
	var buf bytes.Buffer
	warnKeyPerms(keyPermLogger(&buf), writeKey(t, 0o600))
	if buf.Len() != 0 {
		t.Fatalf("a 0600 key must not warn, got %q", buf.String())
	}
}

// No configured key is not a warning: local providers need no key at all.
func TestWarnKeyPermsIgnoresAnEmptyPath(t *testing.T) {
	var buf bytes.Buffer
	warnKeyPerms(keyPermLogger(&buf), "   ")
	if buf.Len() != 0 {
		t.Fatalf("an unset key path must not warn, got %q", buf.String())
	}
}
