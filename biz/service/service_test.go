package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oekit/logx"

	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/device"
	"github.com/yi-nology/open-voice-bridge/internal/llm"
	"github.com/yi-nology/open-voice-bridge/internal/text"
)

type fakeAgent struct {
	connected bool
	ready     bool
	engine    asr.Engine
	status    map[string]any
	address   string
}

func (f *fakeAgent) Status() map[string]any {
	if f.status != nil {
		return f.status
	}
	return map[string]any{
		"connected":       f.connected,
		"ready":           f.ready,
		"last_text":       "你好",
		"asr_local":       false,
		"mic_healthy":     true,
		"last_latency_ms": int64(0),
	}
}

func (f *fakeAgent) Reconnect() error { return nil }
func (f *fakeAgent) SetAddress(address string) error {
	f.address = address
	return nil
}
func (f *fakeAgent) RestartAudio(context.Context) error { return nil }
func (f *fakeAgent) SetEngine(engine asr.Engine)        { f.engine = engine }
func (f *fakeAgent) SetLLM(processor llm.Processor)     {}
func (f *fakeAgent) SetTextRules(rules *text.Rules)     {}
func (f *fakeAgent) ASRName() string {
	if f.engine != nil {
		return f.engine.Name()
	}
	return "fake"
}

type fakeHelper struct {
	status   map[string]any
	applied  map[string]string
	devices  []device.Match
	applyErr error
}

func (f *fakeHelper) Call(ctx context.Context, payload any) (map[string]any, error) {
	command, _ := payload.(map[string]any)
	if command["cmd"] == "apply" && f.applyErr != nil {
		return nil, f.applyErr
	}
	if command["cmd"] == "apply" {
		if mapping, ok := command["mapping"].(map[string]string); ok {
			f.applied = mapping
		}
		return map[string]any{"ok": true, "config": "{}"}, nil
	}
	if command["cmd"] == "set_devices" {
		f.devices, _ = command["devices"].([]device.Match)
		return map[string]any{"ok": true, "devices": len(f.devices)}, nil
	}
	if f.status != nil {
		return f.status, nil
	}
	return map[string]any{
		"keyd_active":    true,
		"remote_present": true,
		"mapping":        map[string]string{"power": "esc"},
		"config":         "{}",
	}, nil
}

func newTestService(t *testing.T) (*Service, *fakeAgent, *fakeHelper, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Default()
	agent := &fakeAgent{connected: true, ready: true}
	helper := &fakeHelper{}
	svc := New(cfg, path, agent, helper, nil, logx.NewSlogLogger("service-test"))
	return svc, agent, helper, path
}

func TestStatusAndReady(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	status := svc.Status(context.Background())
	if status["ok"] != true || status["address"] != config.Default().Address {
		t.Fatalf("status = %+v", status)
	}
	if status["asr_provider"] != "mimo" || status["keymap_profile"] != "coding" {
		t.Fatalf("status = %+v", status)
	}
	ready := svc.Ready(context.Background())
	if ready["ok"] != true || ready["asr_ready"] != true {
		t.Fatalf("ready = %+v", ready)
	}
}

func TestReadyRequiresHealthyMic(t *testing.T) {
	svc, agent, _, _ := newTestService(t)
	agent.status = map[string]any{
		"connected":   true,
		"ready":       true,
		"mic_healthy": false,
	}
	ready := svc.Ready(context.Background())
	if ready["ok"] != false || ready["mic_healthy"] != false {
		t.Fatalf("ready = %+v", ready)
	}
}

func TestApplySavesCustomProfile(t *testing.T) {
	svc, _, _, path := newTestService(t)
	if _, err := svc.Apply(context.Background(), map[string]string{"power": "esc", "f5": "f24"}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Keymap.Profile != "custom" {
		t.Fatalf("profile = %q", loaded.Keymap.Profile)
	}
	if loaded.Keymap.Profiles["custom"]["power"] != "esc" {
		t.Fatalf("custom = %+v", loaded.Keymap.Profiles["custom"])
	}
}

func TestSaveAndDeleteProfile(t *testing.T) {
	svc, _, _, path := newTestService(t)
	mapping := config.DefaultProfiles()["minimal"]
	if _, err := svc.SaveKeymapProfile(context.Background(), "studio", mapping); err != nil {
		t.Fatalf("SaveKeymapProfile: %v", err)
	}
	profile, profiles := svc.KeymapProfiles(context.Background())
	if profile != "studio" || profiles["studio"] == nil {
		t.Fatalf("profiles = %v %v", profile, profiles)
	}
	if err := svc.DeleteKeymapProfile(context.Background(), "coding"); err == nil {
		t.Fatal("built-in profile deletion should fail")
	}
	if _, err := svc.ApplyKeymapProfile(context.Background(), "coding"); err != nil {
		t.Fatalf("ApplyKeymapProfile: %v", err)
	}
	if err := svc.DeleteKeymapProfile(context.Background(), "studio"); err != nil {
		t.Fatalf("DeleteKeymapProfile: %v", err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := loaded.Keymap.Profiles["studio"]; ok {
		t.Fatalf("studio still present: %+v", loaded.Keymap.Profiles)
	}
}

func TestSyncKeymapAppliesConfiguredProfile(t *testing.T) {
	svc, _, helper, _ := newTestService(t)
	svc.SyncKeymap(context.Background())
	if helper.applied == nil || helper.applied["power"] != "esc" {
		t.Fatalf("applied = %+v", helper.applied)
	}
}

func TestDevicesAndSelection(t *testing.T) {
	svc, agent, helper, path := newTestService(t)
	data := svc.Devices(context.Background())
	if data["ok"] != true || len(data["devices"].([]map[string]any)) != 1 {
		t.Fatalf("devices = %+v", data)
	}
	if err := svc.SaveDevice(context.Background(), config.DeviceConfig{
		ID:      "rc003-2",
		Name:    "Second",
		Address: "AA:BB:CC:DD:EE:01",
		Profile: "rc003",
		Enabled: true,
	}); err != nil {
		t.Fatalf("SaveDevice: %v", err)
	}
	if err := svc.SaveDevice(context.Background(), config.DeviceConfig{
		ID:      "generic-1",
		Name:    "Generic",
		Address: "AA:BB:CC:DD:EE:02",
		Profile: "generic-ble-hid",
		Enabled: true,
		Vendor:  "1234",
		Product: "5678",
	}); err != nil {
		t.Fatalf("SaveDevice generic: %v", err)
	}
	if err := svc.SelectDevice(context.Background(), "rc003-2"); err != nil {
		t.Fatalf("SelectDevice: %v", err)
	}
	if agent.address != "AA:BB:CC:DD:EE:01" {
		t.Fatalf("agent address = %q", agent.address)
	}
	svc.SyncDevices(context.Background())
	if len(helper.devices) != 3 {
		t.Fatalf("matchers = %+v", helper.devices)
	}
	if helper.devices[2].Vendor != "1234" || helper.devices[2].Product != "5678" {
		t.Fatalf("generic matcher = %+v", helper.devices[2])
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.ActiveDevice != "rc003-2" {
		t.Fatalf("active = %q", loaded.ActiveDevice)
	}
	if err := svc.DeleteDevice(context.Background(), "rc003-2"); err == nil {
		t.Fatal("deleting active device should fail")
	}
}

func TestLLMConfigService(t *testing.T) {
	svc, _, _, path := newTestService(t)
	data := svc.LLMConfig(context.Background())
	if data["ok"] != true {
		t.Fatalf("llm config = %+v", data)
	}
	if presets, ok := data["presets"].([]llm.Preset); !ok || len(presets) < 3 {
		t.Fatalf("presets = %+v", data["presets"])
	}
	next := llm.Config{Enabled: false, Provider: "minimax", Mode: "cleanup", TimeoutSeconds: 20}
	if err := svc.UpdateLLMConfig(context.Background(), next); err != nil {
		t.Fatalf("UpdateLLMConfig: %v", err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.LLM.Provider != "minimax" {
		t.Fatalf("llm = %+v", loaded.LLM)
	}
	diagnostics := svc.TestLLM(context.Background(), "minimax", false)
	if len(diagnostics) == 0 {
		t.Fatal("no llm diagnostics")
	}
}

func TestSaveLLMKey(t *testing.T) {
	svc, _, _, path := newTestService(t)
	if err := svc.SaveLLMKey(context.Background(), "sk-test\n"); err != nil {
		t.Fatalf("SaveLLMKey: %v", err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.LLM.APIKeyFile == "" {
		t.Fatal("api key path not saved")
	}
	info, err := os.Stat(loaded.LLM.APIKeyFile)
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key perm = %o", info.Mode().Perm())
	}
	data, err := os.ReadFile(loaded.LLM.APIKeyFile)
	if err != nil || string(data) != "sk-test\n" {
		t.Fatalf("key = %q err=%v", data, err)
	}
}

func TestLLMLiveDiagnostic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}]}`))
	}))
	defer server.Close()
	svc, _, _, _ := newTestService(t)
	svc.cfgMu.Lock()
	svc.cfg.LLM = llm.Config{Enabled: true, Provider: "custom", BaseURL: server.URL, Model: "test", Mode: "cleanup", TimeoutSeconds: 5}
	svc.cfgMu.Unlock()
	diagnostics := svc.TestLLM(context.Background(), "", true)
	found := false
	for _, diagnostic := range diagnostics {
		if strings.HasPrefix(diagnostic.Detail, "live:") {
			found = true
		}
	}
	if !found {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
}

func TestTextConfigService(t *testing.T) {
	svc, _, _, path := newTestService(t)
	next := text.Config{
		Dictionary: []text.Replacement{{From: "米授权", To: "MiniMax"}},
		Snippets:   []text.Snippet{{Trigger: "收货地址", Text: "北京市海淀区"}},
	}
	if err := svc.UpdateTextConfig(context.Background(), next); err != nil {
		t.Fatalf("UpdateTextConfig: %v", err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Text.Dictionary) != 1 || len(loaded.Text.Snippets) != 1 {
		t.Fatalf("text = %+v", loaded.Text)
	}
	data := svc.TextConfig(context.Background())
	if data["ok"] != true {
		t.Fatalf("text config = %+v", data)
	}
}
