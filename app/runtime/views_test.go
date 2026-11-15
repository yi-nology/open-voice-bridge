package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yi-nology/open-voice-bridge/biz/service"
	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/core"
)

type stubAgent struct {
	view core.AgentView
	name string
}

func (s stubAgent) Status() core.AgentView { return s.view }
func (s stubAgent) ASRName() string        { return s.name }

type stubHelper struct {
	status core.HelperResponse
	err    error
}

func (s stubHelper) Call(context.Context, core.HelperRequest) (core.HelperResponse, error) {
	return s.status, s.err
}

func readyInputs() service.ViewInputs {
	return service.ViewInputs{
		Cfg: config.Default(),
		Agent: stubAgent{
			view: core.AgentView{Connected: true, Ready: true, MicHealthy: true},
			name: "fake",
		},
		Helper: stubHelper{status: core.HelperResponse{KeydActive: true, RemotePresent: true}},
	}
}

func TestBuildStatusAndReady(t *testing.T) {
	in := readyInputs()
	status := BuildStatus(context.Background(), in)
	if !status.OK || status.Address != config.Default().Address {
		t.Fatalf("status = %+v", status)
	}
	if status.AsrProvider != "mimo" || status.KeymapProfile != "coding" {
		t.Fatalf("status = %+v", status)
	}
	ready := BuildReady(context.Background(), in)
	if !ready.OK || !ready.AsrReady {
		t.Fatalf("ready = %+v", ready)
	}
}

// OPS-06: readiness answers "can dictation work". Core is the ASR chain;
// helper/BLE/mic are optional gaps that must not fail a phone-only setup.
func TestReadyCoreIgnoresOptionalGaps(t *testing.T) {
	in := readyInputs()
	in.Agent = stubAgent{view: core.AgentView{Connected: false, Ready: false, MicHealthy: false}}
	in.Helper = stubHelper{}
	ready := BuildReady(context.Background(), in)
	if !ready.OK || !ready.CoreOK {
		t.Fatalf("core readiness failed on optional gaps: %+v", ready)
	}
	if ready.OptionalOK {
		t.Fatalf("optional_ok = %v, want false with helper/BLE/mic down", ready.OptionalOK)
	}
	if ready.MicHealthy || ready.BleConnected {
		t.Fatalf("individual checks lost: %+v", ready)
	}
}

func TestReadyFailsWhenASRBroken(t *testing.T) {
	in := readyInputs()
	in.ASRErr = errors.New("no ASR provider could be built")
	ready := BuildReady(context.Background(), in)
	if ready.OK || ready.CoreOK {
		t.Fatalf("core readiness passed without ASR: %+v", ready)
	}
	if !ready.OptionalOK {
		t.Fatalf("optional_ok = %v, want true when only ASR is broken", ready.OptionalOK)
	}
}

func TestBuildDevices(t *testing.T) {
	data := BuildDevices(context.Background(), readyInputs())
	if !data.OK || len(data.Devices) != 1 {
		t.Fatalf("devices = %+v", data)
	}
	if data.Devices[0].ID != "rc003" || !data.Devices[0].Connected {
		t.Fatalf("device = %+v", data.Devices[0])
	}
}

// I7: a redacted summary for a public issue carries no device address and
// no home paths — including inside typed views and error strings.
func TestBuildDiagnosticsRedactsAddressAndHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir")
	}
	cfg := config.Default()
	cfg.Address = "AA:BB:CC:DD:EE:FF"
	mimo := cfg.ASR.Providers["mimo"]
	mimo.APIKeyFile = filepath.Join(home, ".config", "xiaomi-mimo", "api_key")
	cfg.ASR.Providers["mimo"] = mimo
	in := readyInputs()
	in.Cfg = cfg
	summary := BuildDiagnostics(context.Background(), in, "test-version")
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

func TestBuildDiagnosticsScrubsHomeFromReadyErrors(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir")
	}
	in := readyInputs()
	in.Helper = stubHelper{err: errors.New("dial " + filepath.Join(home, ".local", "run", "helper.sock"))}
	summary := BuildDiagnostics(context.Background(), in, "test-version")
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), home) {
		t.Fatalf("summary leaked the home directory: %s", encoded)
	}
}

// I7: the report's status strings carry paths too (asr error, helper config,
// helper error) — every one is scrubbed to ~.
func TestBuildDiagnosticsScrubsHomeFromStatusStrings(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir")
	}
	keyPath := filepath.Join(home, ".config", "xiaomi-mimo", "api_key")
	in := readyInputs()
	in.ASRErr = errors.New("read MiMo API key: open " + keyPath + ": permission denied")
	in.Helper = stubHelper{status: core.HelperResponse{
		KeydActive: true,
		Config:     "# loaded from " + filepath.Join(home, ".config", "keyd", "default.conf"),
	}}
	summary := BuildDiagnostics(context.Background(), in, "test-version")
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), home) {
		t.Fatalf("summary leaked the home directory: %s", encoded)
	}
	status, ok := summary["status"].(core.StatusView)
	if !ok {
		t.Fatalf("status = %T, want core.StatusView", summary["status"])
	}
	if !strings.Contains(status.AsrError, "~/.config/xiaomi-mimo/api_key") {
		t.Fatalf("asr error = %q", status.AsrError)
	}
	if !strings.Contains(status.Config, "~/.config/keyd/default.conf") {
		t.Fatalf("config = %q", status.Config)
	}

	in = readyInputs()
	in.Helper = stubHelper{err: errors.New("dial " + filepath.Join(home, ".local", "run", "helper.sock"))}
	summary = BuildDiagnostics(context.Background(), in, "test-version")
	encoded, err = json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), home) {
		t.Fatalf("summary leaked the home directory: %s", encoded)
	}
	status, ok = summary["status"].(core.StatusView)
	if !ok {
		t.Fatalf("status = %T, want core.StatusView", summary["status"])
	}
	if !strings.Contains(status.HelperError, "~/.local/run/helper.sock") {
		t.Fatalf("helper error = %q", status.HelperError)
	}
}

// I7: the report promises "no speech" — the last dictated sentence must not
// ride along to a public issue.
func TestBuildDiagnosticsOmitsSpeech(t *testing.T) {
	const spoken = "明天上午十点开会"
	in := readyInputs()
	in.Agent = stubAgent{
		view: core.AgentView{Connected: true, Ready: true, MicHealthy: true, LastText: spoken},
		name: "fake",
	}
	summary := BuildDiagnostics(context.Background(), in, "test-version")
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), spoken) {
		t.Fatalf("summary leaked the last transcript: %s", encoded)
	}
	status, ok := summary["status"].(core.StatusView)
	if !ok {
		t.Fatalf("status = %T", summary["status"])
	}
	if status.AsrProvider != "mimo" {
		t.Fatalf("status lost its provider: %+v", status)
	}
}
