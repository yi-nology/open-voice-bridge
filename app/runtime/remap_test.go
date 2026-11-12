package runtime_test

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oekit/logx"

	"github.com/yi-nology/open-voice-bridge/app/runtime"
)

// The remapper grabs /dev/input and writes /dev/uinput. Without root it must
// refuse before constructing anything, not half-start and fail on the first
// device — a leftover socket would then look like a running helper.
func TestRunRemapRefusesWithoutRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root would start the daemon; this covers the refusal")
	}
	dir := t.TempDir()
	socket := filepath.Join(dir, "helper.sock")
	var buf bytes.Buffer
	logger := logx.NewSlogLogger("remap-test", logx.WithHandler(slog.NewTextHandler(&buf, nil)))

	code := runtime.RunRemap(runtime.RemapOptions{
		MappingPath: filepath.Join(dir, "mapping.json"),
		SocketPath:  socket,
		SocketGroup: 1000,
		Logger:      logger,
	})
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if logged := buf.String(); !strings.Contains(logged, "root") {
		t.Fatalf("log = %q, want the root requirement named", logged)
	}
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Fatalf("a refused remap must not leave a socket behind (err=%v)", err)
	}
}
