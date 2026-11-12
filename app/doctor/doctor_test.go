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

// OPS-03: doctor is the command the README sends people to first, so it must
// not create or migrate the config behind their back.
func TestRunDoesNotWriteTheConfig(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "absent.json")
	var out bytes.Buffer
	if code := Run(missing, quietLogger(), &out); code != 1 {
		t.Fatalf("exit = %d, want 1 for a missing config\n%s", code, out.String())
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("doctor created the config file (stat err = %v)", err)
	}
	if !strings.Contains(out.String(), "FAIL: config") {
		t.Fatalf("report must name the missing config\n%s", out.String())
	}

	// A pre-sections document is exactly what config.Load migrates in place;
	// doctor must leave those bytes alone.
	legacy := filepath.Join(dir, "legacy.json")
	if err := os.WriteFile(legacy, []byte(`{"address":"11:22:33:44:55:66"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(legacy)
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	Run(legacy, quietLogger(), &out)
	after, err := os.ReadFile(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("doctor rewrote the config:\nbefore %s\nafter  %s", before, after)
	}
}

// OPS-03: the example address passes Validate's shape check but is not a
// remote. doctor must say "not configured" instead of printing it as if set.
func TestRunWarnsOnThePlaceholderAddress(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "mimo.key")
	if err := os.WriteFile(keyFile, []byte("test-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := mimoConfig(t, keyFile)
	var out bytes.Buffer
	Run(path, quietLogger(), &out)
	if !strings.Contains(out.String(), "WARN: address is still the example") {
		t.Fatalf("report must flag the placeholder address\n%s", out.String())
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Address = "11:22:33:44:55:66"
	for index := range cfg.Devices {
		cfg.Devices[index].Address = cfg.Address
	}
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	Run(path, quietLogger(), &out)
	if strings.Contains(out.String(), "WARN: address is still the example") {
		t.Fatalf("a configured address must not warn\n%s", out.String())
	}
}

// lineContaining returns the single report line carrying needle, so a test can
// assert on one check without matching the whole report.
func lineContaining(t *testing.T, report, needle string) string {
	t.Helper()
	var found []string
	for _, line := range strings.Split(report, "\n") {
		if strings.Contains(line, needle) {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly one line containing %q, got %d\n%s", needle, len(found), report)
	}
	return found[0]
}

// configWithHelper points a valid MiMo document at a helper socket, so a test
// can decide whether the root remapper is reachable.
func configWithHelper(t *testing.T, keyFile, socket string) string {
	t.Helper()
	path := mimoConfig(t, keyFile)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.HelperSocket = socket
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	return path
}

func testKeyFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mimo.key")
	if err := os.WriteFile(path, []byte("test-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// OPS-04: doctor and /readyz must agree on what dictation needs. The root
// remapper is optional — a phone-only setup never has one — so its absence is a
// warning carrying a command to run, not a failed run.
func TestRunWarnsWithAFixWhenTheHelperIsAbsent(t *testing.T) {
	path := configWithHelper(t, testKeyFile(t), filepath.Join(t.TempDir(), "absent.sock"))
	var out bytes.Buffer
	code := Run(path, quietLogger(), &out)
	report := out.String()
	if code != 0 {
		t.Fatalf("exit = %d, want 0: an absent helper is optional (OPS-06)\n%s", code, report)
	}
	line := lineContaining(t, report, "remap helper")
	if !strings.HasPrefix(line, "WARN:") {
		t.Fatalf("helper line = %q, want a WARN", line)
	}
	if !strings.Contains(line, "systemctl") || !strings.Contains(line, "xiaomi-remote-remap") {
		t.Fatalf("helper line must name a command to run: %q", line)
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
