package doctor

import (
	"bytes"
	"strings"
	"testing"

	"github.com/yi-nology/open-voice-bridge/internal/core"
)

// OPS-04: a diagnosis without a next step leaves the user guessing, so every
// gap carries the command that resolves it. An ok line has nothing to fix.
func TestReportPrintsTheFixOnlyForGaps(t *testing.T) {
	var out bytes.Buffer
	failures := report(&out, []check{
		ok("pw-cat"),
		warn("remap helper: dial unix: no such file", "sudo systemctl start xiaomi-remote-remap.service"),
	})
	reported := out.String()
	if failures != 0 {
		t.Fatalf("failures = %d, want 0 for warnings", failures)
	}
	for _, want := range []string{
		"ok: pw-cat\n",
		"WARN: remap helper: dial unix: no such file; fix: sudo systemctl start xiaomi-remote-remap.service\n",
	} {
		if !strings.Contains(reported, want) {
			t.Errorf("report missing %q\n%s", want, reported)
		}
	}
	if strings.Contains(reported, "ok: pw-cat; fix:") {
		t.Errorf("an ok line must not carry a fix\n%s", reported)
	}
}

// The severity of a gap comes from the shared yardstick, not from the probe:
// doctor must exit non-zero exactly when /readyz's ok is false (OPS-04).
func TestGapSeverityFollowsTheSharedYardstick(t *testing.T) {
	var out bytes.Buffer
	failures := report(&out, []check{
		gap(core.DepASR, "asr/mimo: key missing", "install -m 600 /dev/null key"),
		gap(core.DepHelper, "remap helper: down", "sudo systemctl start xiaomi-remote-remap.service"),
		gap(core.DepMic, "missing pw-cat", "sudo apt install pipewire-bin"),
	})
	if failures != 1 {
		t.Fatalf("failures = %d, want 1: only the ASR chain is core\n%s", failures, out.String())
	}
	reported := out.String()
	if !strings.Contains(reported, "FAIL: asr/mimo: key missing") {
		t.Errorf("a core gap must FAIL\n%s", reported)
	}
	for _, optional := range []string{"WARN: remap helper: down", "WARN: missing pw-cat"} {
		if !strings.Contains(reported, optional) {
			t.Errorf("an optional gap must WARN, missing %q\n%s", optional, reported)
		}
	}
}
