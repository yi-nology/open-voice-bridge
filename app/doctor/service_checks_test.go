package doctor

import (
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/yi-nology/open-voice-bridge/internal/config"
)

// OPS-04: the phone listener and both TCP surfaces are part of the report.
func TestConfigChecksPhoneDisabledAndPortFree(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	cfg := config.Default()
	cfg.UIAddr = "127.0.0.1:" + strconv.Itoa(port)
	cfg.Phone.Enabled = false
	checks := configChecks(cfg, filepath.Join(t.TempDir(), "config.json"))
	joined := textOf(checks)
	if !strings.Contains(joined, "phone input disabled") {
		t.Fatalf("missing phone-disabled note: %s", joined)
	}
	if !strings.Contains(joined, "free") {
		t.Fatalf("missing free-port note: %s", joined)
	}
}

func TestConfigChecksPhoneEnabledReportsListener(t *testing.T) {
	cfg := config.Default()
	cfg.Phone.Enabled = true
	cfg.Phone.Addr = "127.0.0.1:0"
	dir := t.TempDir()
	checks := configChecks(cfg, filepath.Join(dir, "config.json"))
	joined := textOf(checks)
	if !strings.Contains(joined, "phone listener") || !strings.Contains(joined, "cert dir writable") {
		t.Fatalf("phone checks missing: %s", joined)
	}
}

func textOf(checks []check) string {
	var b strings.Builder
	for _, item := range checks {
		b.WriteString(item.text)
		b.WriteString("\n")
	}
	return b.String()
}
