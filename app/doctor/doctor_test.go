package doctor

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oekit/logx"

	"github.com/yi-nology/open-voice-bridge/internal/config"
)

func quietLogger() logx.Logger {
	return logx.NewSlogLogger("doctor-test", logx.WithHandler(slog.NewTextHandler(io.Discard, nil)))
}

// mimoConfig writes a document whose only provider is MiMo pointed at keyFile.
// The fallback chain is cleared so the report covers exactly one provider.
func mimoConfig(t *testing.T, keyFile string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	cfg.ASR.Provider = "mimo"
	cfg.ASR.Fallback = nil
	cfg.ASR.Providers = map[string]config.ASRProviderConfig{
		"mimo": {Type: "mimo", APIKeyFile: keyFile},
	}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	return path
}

// A missing key is the most common first-run failure, and doctor is the entry
// point the README sends people to. It must name the file and exit non-zero.
func TestRunFailsWhenTheProviderKeyIsMissing(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "absent.key")
	var out bytes.Buffer
	if code := Run(mimoConfig(t, absent), quietLogger(), &out); code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", code, out.String())
	}
	report := out.String()
	for _, want := range []string{"FAIL: asr/mimo", "FAIL: API key " + absent} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q\n%s", want, report)
		}
	}
}

// A key that is present and owner-only is reported as ok, with the byte count
// and mode so the user can see which file was actually read.
func TestRunReportsAnOwnerOnlyKeyAsOK(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "mimo.key")
	if err := os.WriteFile(keyFile, []byte("test-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	Run(mimoConfig(t, keyFile), quietLogger(), &out)
	report := out.String()
	if !strings.Contains(report, "ok: API key "+keyFile+" (8 bytes, 0600)") {
		t.Errorf("report missing the key summary\n%s", report)
	}
	if strings.Contains(report, "WARN: API key is readable") {
		t.Errorf("0600 key warned about permissions\n%s", report)
	}
}

// A world-readable key is a warning, not a failure: the user may be on a
// single-account machine, and doctor must not refuse to report anything else.
func TestRunWarnsOnAGroupReadableKey(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "mimo.key")
	if err := os.WriteFile(keyFile, []byte("test-key"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	Run(mimoConfig(t, keyFile), quietLogger(), &out)
	if report := out.String(); !strings.Contains(report, "WARN: API key is readable by group/other") {
		t.Errorf("report missing the permission warning\n%s", report)
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	for _, tc := range []struct{ in, want string }{
		{"~/keys/mimo.key", filepath.Join(home, "keys/mimo.key")},
		{"/etc/mimo.key", "/etc/mimo.key"},
		{"", ""},
		{"~other/mimo.key", "~other/mimo.key"},
	} {
		if got := expandHome(tc.in); got != tc.want {
			t.Errorf("expandHome(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
