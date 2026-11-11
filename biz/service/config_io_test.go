package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oekit/logx"

	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/core"
)

func testService(t *testing.T) *Service {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load default config: %v", err)
	}
	return New(cfg, path, &fakeAgent{}, &fakeHelper{status: map[string]any{"keyd_active": true}}, nil, logx.Delegating("test"))
}

func TestExportConfigRoundTrip(t *testing.T) {
	svc := testService(t)
	data, err := svc.ExportConfig()
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if !strings.Contains(string(data), `"ui_addr"`) {
		t.Fatalf("export = %s, want the full config", data)
	}
	if err := svc.ImportConfig(data); err != nil {
		t.Fatalf("import round trip: %v", err)
	}
}

func TestImportConfigRejectsInvalidAndKeepsBackup(t *testing.T) {
	svc := testService(t)
	before, err := os.ReadFile(svc.path)
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.ImportConfig([]byte("{ not json")); err == nil {
		t.Fatal("malformed import accepted")
	}
	if err := svc.ImportConfig([]byte(`{"address":"nonsense"}`)); err == nil {
		t.Fatal("invalid import accepted")
	}
	after, err := os.ReadFile(svc.path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("rejected import must leave the config file untouched")
	}

	// A valid import replaces the file and keeps a timestamped backup.
	mutated := map[string]any{}
	if err := json.Unmarshal(before, &mutated); err != nil {
		t.Fatal(err)
	}
	mutated["paste_enabled"] = false
	payload, _ := json.Marshal(mutated)
	if err := svc.ImportConfig(payload); err != nil {
		t.Fatalf("valid import: %v", err)
	}
	entries, err := filepath.Glob(svc.path + ".bak-*")
	if err != nil || len(entries) == 0 {
		t.Fatalf("backup missing: %v %v", entries, err)
	}
	saved, _ := os.ReadFile(svc.path)
	if strings.Contains(string(saved), `"paste_enabled": true`) {
		t.Fatal("import did not take effect")
	}
}

func TestDiagnosticsRedactsAddressAndHome(t *testing.T) {
	svc := testService(t)
	svc.cfg.Address = "AA:BB:CC:DD:EE:FF"
	home, _ := os.UserHomeDir()
	if home == "" {
		t.Skip("no home dir")
	}
	svc.cfg.APIKeyFile = filepath.Join(home, ".config", "xiaomi-mimo", "api_key")

	summary := svc.Diagnostics(context.Background(), "test-version")
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	body := string(encoded)
	if strings.Contains(body, "AA:BB:CC:DD:EE:FF") {
		t.Fatalf("summary leaked the device address: %s", body)
	}
	if !strings.Contains(body, "AA:BB:CC:**:**:FF") {
		t.Fatalf("summary should carry the redacted address: %s", body)
	}
	if strings.Contains(body, home) {
		t.Fatalf("summary leaked the home directory: %s", body)
	}
	if summary["version"] != "test-version" {
		t.Fatalf("version = %v", summary["version"])
	}
}

// I7: the readiness view is typed, so scrubHome no longer walks into it. Its
// error strings must still be scrubbed -- a helper socket path under the home
// directory would otherwise reach a report the user attaches to a public issue.
func TestDiagnosticsScrubsHomeFromReadyErrors(t *testing.T) {
	home, _ := os.UserHomeDir()
	if home == "" {
		t.Skip("no home dir")
	}
	svc := testService(t)
	svc.helper = &fakeHelper{callErr: errors.New("dial " + filepath.Join(home, ".local", "run", "helper.sock"))}

	summary := svc.Diagnostics(context.Background(), "test-version")
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), home) {
		t.Fatalf("summary leaked the home directory: %s", encoded)
	}
	ready, ok := summary["ready"].(core.ReadyView)
	if !ok {
		t.Fatalf("ready = %T, want core.ReadyView", summary["ready"])
	}
	if !strings.Contains(ready.HelperError, "~/.local/run/helper.sock") {
		t.Fatalf("helper error = %q", ready.HelperError)
	}
	// The report's keys are part of its shape; typing the view must not turn
	// them into Go field names.
	if !strings.Contains(string(encoded), `"keyd_active":`) {
		t.Fatalf("ready lost its snake_case keys: %s", encoded)
	}
}

// I7: both Diagnostics doc comments promise "no speech", but the report embedded
// the whole status map, which carries the last dictated sentence. Attaching it
// to a public issue would publish what the user last said.
func TestDiagnosticsOmitsSpeech(t *testing.T) {
	svc, agent, _, _ := newTestService(t)
	const spoken = "明天上午十点开会"
	agent.view = &core.AgentView{Connected: true, Ready: true, MicHealthy: true, LastText: spoken}

	summary := svc.Diagnostics(context.Background(), "test-version")
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), spoken) {
		t.Fatalf("summary leaked the last transcript: %s", encoded)
	}
	// The rest of the status still has to be there, or the report is useless.
	status, ok := summary["status"].(map[string]any)
	if !ok {
		t.Fatalf("status = %T", summary["status"])
	}
	if status["asr_provider"] != "mimo" {
		t.Fatalf("status lost its provider: %+v", status)
	}
}

// DOM-11: a keymap change must not reach the root helper before the config
// saves. Otherwise a failed save leaves the daemon running a mapping that
// exists nowhere on disk.
func TestKeymapApplyFailsClosedWhenSaveFails(t *testing.T) {
	svc := testService(t)
	helper := svc.helper.(*fakeHelper)
	// Rename-onto-a-directory makes config.Save fail after validation.
	svc.path = t.TempDir()

	if _, err := svc.Apply(context.Background(), map[string]string{"up": "up"}); err == nil {
		t.Fatal("apply must fail when the config cannot be saved")
	}
	if applied := helper.appliedSnapshot(); len(applied) != 0 {
		t.Fatalf("helper updated before the config saved: %v", applied)
	}
	if _, err := svc.SaveKeymapProfile(context.Background(), "custom-x", map[string]string{"up": "up"}); err == nil {
		t.Fatal("save profile must fail when the config cannot be saved")
	}
	if applied := helper.appliedSnapshot(); len(applied) != 0 {
		t.Fatalf("helper updated by SaveKeymapProfile before the config saved: %v", applied)
	}
}

// 切得准（SayAll 按 App 自动匹配）: the focused window class picks a keymap
// profile at runtime — the configured profile is never overwritten.
func TestApplyAppRuleSwitchesRuntimeMapping(t *testing.T) {
	svc := testService(t)
	helper := svc.helper.(*fakeHelper)
	svc.cfg.Keymap.Profiles["media"] = map[string]string{"enter": "playpause"}
	svc.cfg.Keymap.AppRules = []config.AppRule{
		{Class: "Firefox", Profile: "media"},
		{Class: "code-oss", Profile: "minimal"},
	}

	svc.applyAppRule(context.Background(), "Navigator/firefox") // substring, case-insensitive
	applied := helper.appliedSnapshot()
	if applied["enter"] != "playpause" {
		t.Fatalf("applied = %v, want the media profile's mapping", applied)
	}

	svc.applyAppRule(context.Background(), "kitty") // no rule: back to the configured profile
	applied = helper.appliedSnapshot()
	if applied["enter"] == "playpause" {
		t.Fatalf("unmatched class must fall back to the configured profile: %v", applied)
	}
}
