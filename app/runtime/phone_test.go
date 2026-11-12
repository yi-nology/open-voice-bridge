package runtime_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yi-nology/open-voice-bridge/app/runtime"
	"github.com/yi-nology/open-voice-bridge/internal/agent"
	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/input"
	"github.com/yi-nology/open-voice-bridge/internal/phone"
	"github.com/yi-nology/open-voice-bridge/internal/text"
)

type stubEngine struct {
	result   asr.Result
	err      error
	captured []asr.Audio
}

func (e *stubEngine) Name() string                { return "stub" }
func (e *stubEngine) Local() bool                 { return true }
func (e *stubEngine) Close(context.Context) error { return nil }

func (e *stubEngine) Transcribe(_ context.Context, audio asr.Audio) (asr.Result, error) {
	e.captured = append(e.captured, audio)
	return e.result, e.err
}

func TestPhoneSpeechMapsTheTranscript(t *testing.T) {
	engine := &stubEngine{result: asr.Result{
		Text:     "帮我写个测试",
		Provider: "stub",
		Latency:  1300 * time.Millisecond,
		Language: "zh",
	}}
	rules, err := text.New(config.TextConfig{Dictionary: []config.Replacement{{From: "测试", To: "unittest"}}})
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	ag := agent.New(config.Config{MaxUtterance: 30}, engine, nil)
	ag.SetTextRules(rules)
	adapter := runtime.PhoneSpeech{Pipeline: ag.Speech()}

	result, ok := adapter.TranscribeUtterance(context.Background(), make([]int16, 16000), phone.UtteranceMeta{
		DeviceID: "phone-abc", Name: "Pixel", Address: "192.0.2.10",
	})
	if !ok {
		t.Fatal("a full utterance must transcribe")
	}
	if result.Text != "帮我写个unittest" {
		t.Fatalf("text = %q, want the dictionary rewrite applied", result.Text)
	}
	if result.RawText != "帮我写个测试" {
		t.Fatalf("raw_text = %q, want the ASR output unchanged", result.RawText)
	}
	if !result.Processed || result.Provider != "stub" || result.LatencyMS != 1300 {
		t.Fatalf("result = %+v", result)
	}
	if result.At.IsZero() {
		t.Fatal("result must carry a timestamp")
	}
	if len(engine.captured) != 1 || engine.captured[0].SampleRate != 16000 {
		t.Fatalf("engine received %+v, want one 16 kHz request", engine.captured)
	}
}

func TestPhoneSpeechDeclinesSilence(t *testing.T) {
	engine := &stubEngine{result: asr.Result{Text: "x", Provider: "stub"}}
	ag := agent.New(config.Config{MaxUtterance: 30}, engine, nil)
	if _, ok := (runtime.PhoneSpeech{Pipeline: ag.Speech()}).TranscribeUtterance(
		context.Background(), make([]int16, 100), phone.UtteranceMeta{Name: "Pixel"}); ok {
		t.Fatal("audio shorter than the 250 ms floor must not transcribe")
	}
	if len(engine.captured) != 0 {
		t.Fatal("ASR must not be called for silence")
	}
}

// A disabled phone listener leaves Store and Server nil, so every payload must
// answer from the flag alone instead of dereferencing them.
func TestPhoneAdminDisabledNeedsNoStore(t *testing.T) {
	admin := runtime.PhoneAdmin{Enabled: false, Hub: input.NewHub(), Configured: func() bool { return false }}
	ctx := context.Background()

	inputs := admin.Inputs(ctx)
	if inputs["enabled"] != false {
		t.Fatalf("inputs = %+v", inputs)
	}
	if pairing := admin.Pairing(ctx); pairing["enabled"] != false {
		t.Fatalf("pairing = %+v", pairing)
	}
	devices := admin.Devices(ctx)
	if devices["enabled"] != false {
		t.Fatalf("devices = %+v", devices)
	}
	if list, ok := devices["devices"].([]phone.Device); !ok || len(list) != 0 {
		t.Fatalf("devices list = %#v, want an empty typed slice", devices["devices"])
	}
	revoked := admin.Revoke(ctx, "phone-abc")
	if revoked["ok"] != false || revoked["error"] != "phone input disabled" {
		t.Fatalf("revoke = %+v", revoked)
	}
}

// OPS-01: the panel shows the saved intent beside the running state, so a
// config that enables the phone reads as configured before the restart lands.
func TestPhoneAdminInputsSeparatesSavedIntentFromRunning(t *testing.T) {
	admin := runtime.PhoneAdmin{
		Enabled:    false,
		Hub:        input.NewHub(),
		Configured: func() bool { return true },
	}
	inputs := admin.Inputs(context.Background())
	if inputs["enabled"] != false {
		t.Fatalf("running state = %+v, want the listener off", inputs)
	}
	if inputs["configured"] != true {
		t.Fatalf("saved intent lost: %+v", inputs)
	}
}

func newPhoneAdmin(t *testing.T, enabled bool) (runtime.PhoneAdmin, *phone.Store) {
	t.Helper()
	store, err := phone.NewStore(filepath.Join(t.TempDir(), "phone_tokens.json"), phone.Config{
		PairCodeTTL: time.Minute,
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	hub := input.NewHub()
	server := phone.NewServer(store, nil, hub, phone.ServerConfig{TLSMode: "off"})
	return runtime.PhoneAdmin{
		Enabled:    enabled,
		Configured: func() bool { return enabled },
		Store:      store,
		Server:     server,
		Hub:        hub,
		TLSMode:    "off",
		Addr:       "0.0.0.0:8788",
	}, store
}

func TestPhoneAdminPairingOffersThePhoneLink(t *testing.T) {
	admin, _ := newPhoneAdmin(t, true)
	pairing := admin.Pairing(context.Background())
	if pairing["enabled"] != true {
		t.Fatalf("pairing = %+v", pairing)
	}
	code, _ := pairing["code"].(string)
	if code == "" {
		t.Fatalf("pairing must carry a code: %+v", pairing)
	}
	if pairing["scheme"] != "http" || pairing["port"] != "8788" {
		t.Fatalf("scheme/port = %v/%v, want http/8788 for tls=off", pairing["scheme"], pairing["port"])
	}
	expires, _ := pairing["expires_at"].(string)
	if _, err := time.Parse(time.RFC3339, expires); err != nil {
		t.Fatalf("expires_at = %q, want RFC3339: %v", expires, err)
	}
	host, _ := pairing["host"].(string)
	link, _ := pairing["link"].(string)
	if host == "" {
		if link != "" {
			t.Fatalf("no LAN address means no link, got %q", link)
		}
		return
	}
	if link != "http://"+host+":8788/phone" {
		t.Fatalf("link = %q, want the pairing page on the LAN address", link)
	}
}

func TestPhoneAdminRevokeDropsTheToken(t *testing.T) {
	admin, store := newPhoneAdmin(t, true)
	ctx := context.Background()
	code, _ := store.GeneratePairCode()
	deviceID, token, err := store.Pair(code, "Pixel", "192.0.2.10")
	if err != nil {
		t.Fatalf("pair: %v", err)
	}

	revoked := admin.Revoke(ctx, deviceID)
	if revoked["ok"] != true {
		t.Fatalf("revoke = %+v", revoked)
	}
	if _, alive := store.Authenticate(token); alive {
		t.Fatal("a revoked token must stop authenticating")
	}
	for _, device := range admin.Devices(ctx)["devices"].([]phone.Device) {
		if device.ID == deviceID {
			t.Fatalf("revoked device still listed: %+v", device)
		}
	}
}

func TestPhoneAdminRevokeReportsUnknownDevice(t *testing.T) {
	admin, _ := newPhoneAdmin(t, true)
	revoked := admin.Revoke(context.Background(), "phone-missing")
	if revoked["ok"] != false {
		t.Fatalf("revoke = %+v", revoked)
	}
	message, _ := revoked["error"].(string)
	if !strings.Contains(message, "phone-missing") {
		t.Fatalf("error = %q, want the device id named", message)
	}
}

func TestPhoneAdminListsPairedAndConnected(t *testing.T) {
	admin, store := newPhoneAdmin(t, true)
	code, _ := store.GeneratePairCode()
	deviceID, _, err := store.Pair(code, "Pixel", "192.0.2.10")
	if err != nil {
		t.Fatalf("pair: %v", err)
	}
	payload := admin.Devices(context.Background())
	if payload["enabled"] != true {
		t.Fatalf("devices = %+v", payload)
	}
	devices, ok := payload["devices"].([]phone.Device)
	if !ok || len(devices) != 1 || devices[0].ID != deviceID {
		t.Fatalf("devices = %#v, want the paired phone", payload["devices"])
	}
	connected, ok := payload["connected"].([]input.Status)
	if !ok || len(connected) != 0 {
		t.Fatalf("connected = %#v, want an empty typed slice with no live socket", payload["connected"])
	}
}
