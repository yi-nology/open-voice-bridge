package router_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"

	oeerrors "github.com/oekit/pkg/errors"

	apihandler "github.com/yi-nology/open-voice-bridge/biz/handler/api"
	api "github.com/yi-nology/open-voice-bridge/biz/model/api"
	"github.com/yi-nology/open-voice-bridge/biz/router"
)

type fakeBackend struct {
	applied  map[string]string
	applyErr error
	asr      *api.ASRConfig
	ready    *api.ReadyResp
	profiles *api.KeymapProfilesResp
}

func (f *fakeBackend) Health(context.Context) (*api.HealthResp, error) {
	return &api.HealthResp{Ok: true}, nil
}

func (f *fakeBackend) Ready(context.Context) (*api.ReadyResp, error) {
	if f.ready != nil {
		return f.ready, nil
	}
	return &api.ReadyResp{Ok: true}, nil
}

func (f *fakeBackend) Status(context.Context) (*api.StatusResp, error) {
	return &api.StatusResp{
		Ok:             true,
		Address:        "AA:BB:CC:DD:EE:FF",
		KeydActive:     true,
		RemotePresent:  true,
		Mapping:        map[string]string{"power": "esc"},
		AsrProvider:    "mimo",
		AsrProviders:   []string{"mimo"},
		KeymapProfile:  "coding",
		KeymapProfiles: []string{"coding", "media"},
		Capturing:      false,
	}, nil
}

func (f *fakeBackend) Apply(_ context.Context, mapping map[string]string) (*api.OperationResp, error) {
	if f.applyErr != nil {
		return nil, f.applyErr
	}
	f.applied = mapping
	config := "{}"
	return &api.OperationResp{Ok: true, Config: &config}, nil
}

func (f *fakeBackend) CaptureStart(context.Context) (*api.OperationResp, error) {
	return &api.OperationResp{Ok: true}, nil
}

func (f *fakeBackend) CaptureStop(context.Context) (*api.OperationResp, error) {
	return &api.OperationResp{Ok: true}, nil
}

func (f *fakeBackend) Reconnect(context.Context) (*api.OperationResp, error) {
	return &api.OperationResp{Ok: true}, nil
}

func (f *fakeBackend) RestartMic(context.Context) (*api.OperationResp, error) {
	return &api.OperationResp{Ok: true}, nil
}

func (f *fakeBackend) SwitchASR(context.Context, string) (*api.OperationResp, error) {
	return &api.OperationResp{Ok: true}, nil
}

func (f *fakeBackend) GetASRConfig(context.Context) (*api.ASRConfigResp, error) {
	return &api.ASRConfigResp{
		Ok:       true,
		Provider: "mimo",
		Providers: map[string]*api.ProviderConfig{
			"mimo": {Type: "mimo", Model: "mimo-v2.5-asr"},
		},
	}, nil
}

func (f *fakeBackend) UpdateASRConfig(_ context.Context, next *api.ASRConfig) (*api.OperationResp, error) {
	f.asr = next
	return &api.OperationResp{Ok: true}, nil
}

func (f *fakeBackend) TestASR(context.Context, string) (*api.ASRTestResp, error) {
	return &api.ASRTestResp{
		Ok:       true,
		Provider: "mimo",
		Diagnostics: []*api.Diagnostic{
			{Provider: "mimo", Ok: true, Detail: "ok"},
		},
	}, nil
}

func (f *fakeBackend) KeymapProfiles(context.Context) (*api.KeymapProfilesResp, error) {
	if f.profiles != nil {
		return f.profiles, nil
	}
	return &api.KeymapProfilesResp{
		Ok:       true,
		Profile:  "coding",
		Profiles: map[string]map[string]string{"coding": {"power": "esc"}},
	}, nil
}

func (f *fakeBackend) ApplyKeymapProfile(context.Context, string) (*api.OperationResp, error) {
	return &api.OperationResp{Ok: true}, nil
}

func (f *fakeBackend) SaveKeymapProfile(context.Context, string, map[string]string) (*api.OperationResp, error) {
	return &api.OperationResp{Ok: true}, nil
}

func (f *fakeBackend) DeleteKeymapProfile(context.Context, string) (*api.OperationResp, error) {
	return &api.OperationResp{Ok: true}, nil
}

func newTestEngine(backend *fakeBackend) *server.Hertz {
	apihandler.SetBackend(backend)
	h := server.New(server.WithHandleMethodNotAllowed(true))
	router.GeneratedRegister(h)
	return h
}

func perform(h *server.Hertz, method, path, body string) *ut.ResponseRecorder {
	return ut.PerformRequest(h.Engine, method, path,
		&ut.Body{Body: strings.NewReader(body), Len: len(body)},
		ut.Header{Key: "Content-Type", Value: "application/json"},
	)
}

func TestGeneratedRoutes(t *testing.T) {
	backend := &fakeBackend{}
	h := newTestEngine(backend)

	resp := perform(h, "GET", "/healthz", "")
	if resp.Code != 200 || !strings.Contains(resp.Body.String(), `"ok":true`) {
		t.Fatalf("health: code=%d body=%s", resp.Code, resp.Body.String())
	}

	resp = perform(h, "GET", "/api/status", "")
	if resp.Code != 200 {
		t.Fatalf("status code=%d body=%s", resp.Code, resp.Body.String())
	}
	var status api.StatusResp
	if err := json.Unmarshal(resp.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.Address != "AA:BB:CC:DD:EE:FF" || status.Mapping["power"] != "esc" {
		t.Fatalf("status = %+v", status)
	}

	resp = perform(h, "POST", "/api/apply", `{"mapping":{"power":"esc","f5":"f24"}}`)
	if resp.Code != 200 || backend.applied["f5"] != "f24" {
		t.Fatalf("apply: code=%d body=%s mapping=%v", resp.Code, resp.Body.String(), backend.applied)
	}

	resp = perform(h, "POST", "/api/asr/config", `{"provider":"local","fallback":["mimo"],"language":"zh","providers":{"local":{"type":"command","binary":"/bin/echo","threads":2}}}`)
	if resp.Code != 200 || backend.asr == nil || backend.asr.Providers["local"].Binary != "/bin/echo" {
		t.Fatalf("asr update: code=%d body=%s asr=%+v", resp.Code, resp.Body.String(), backend.asr)
	}

	resp = perform(h, "GET", "/api/keymap/profiles", "")
	if resp.Code != 200 || !strings.Contains(resp.Body.String(), `"profile":"coding"`) {
		t.Fatalf("profiles: code=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestReadyFailureStatusCode(t *testing.T) {
	h := newTestEngine(&fakeBackend{ready: &api.ReadyResp{Ok: false, AsrReady: false}})
	resp := perform(h, "GET", "/readyz", "")
	if resp.Code != 503 {
		t.Fatalf("ready code=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestUnknownRoute(t *testing.T) {
	h := newTestEngine(&fakeBackend{})
	resp := perform(h, "GET", "/api/does-not-exist", "")
	if resp.Code != 404 {
		t.Fatalf("unknown route code=%d", resp.Code)
	}
}

func TestWrongMethod(t *testing.T) {
	h := newTestEngine(&fakeBackend{})
	resp := perform(h, "GET", "/api/apply", "")
	if resp.Code != 405 {
		t.Fatalf("wrong method code=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestHandlerErrorEnvelope(t *testing.T) {
	h := newTestEngine(&fakeBackend{applyErr: oeerrors.New("helper unavailable")})
	resp := perform(h, "POST", "/api/apply", `{"mapping":{"power":"esc"}}`)
	if resp.Code != 502 {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if !strings.Contains(body, `"message":"helper unavailable"`) || !strings.Contains(body, `"trace_id"`) {
		t.Fatalf("body=%s", body)
	}
}

func (f *fakeBackend) GetDevices(context.Context) (*api.DevicesResp, error) {
	return &api.DevicesResp{Ok: true, Active: "rc003", Devices: []*api.DeviceInfo{{ID: "rc003", Name: "RC003", Active: true}}}, nil
}

func (f *fakeBackend) SelectDevice(context.Context, string) (*api.OperationResp, error) {
	return &api.OperationResp{Ok: true}, nil
}

func (f *fakeBackend) ToggleDevice(context.Context, string, bool) (*api.OperationResp, error) {
	return &api.OperationResp{Ok: true}, nil
}

func (f *fakeBackend) SaveDevice(context.Context, *api.DeviceSaveReq) (*api.OperationResp, error) {
	return &api.OperationResp{Ok: true}, nil
}

func (f *fakeBackend) DeleteDevice(context.Context, string) (*api.OperationResp, error) {
	return &api.OperationResp{Ok: true}, nil
}

func (f *fakeBackend) DiscoverDevices(context.Context) (*api.DiscoveredResp, error) {
	return &api.DiscoveredResp{Ok: true, Devices: []*api.DiscoveredDevice{{Address: "AA:BB:CC:DD:EE:FF", Name: "RC003", Profile: "rc003"}}}, nil
}

func (f *fakeBackend) ConnectDevice(context.Context, string, bool) (*api.OperationResp, error) {
	return &api.OperationResp{Ok: true}, nil
}

func (f *fakeBackend) GetLLMConfig(context.Context) (*api.LLMConfigResp, error) {
	return &api.LLMConfigResp{Ok: true, Config: &api.LLMConfig{Provider: "minimax"}, Presets: []*api.LLMPreset{{ID: "minimax", Name: "MiniMax"}}}, nil
}

func (f *fakeBackend) UpdateLLMConfig(context.Context, *api.LLMConfig) (*api.OperationResp, error) {
	return &api.OperationResp{Ok: true}, nil
}

func (f *fakeBackend) TestLLM(context.Context, string, bool) (*api.LLMTestResp, error) {
	return &api.LLMTestResp{Ok: true, Diagnostics: []*api.LLMDiagnostic{{Ok: true, Detail: "ok"}}}, nil
}

func (f *fakeBackend) SaveLLMKey(context.Context, string) (*api.OperationResp, error) {
	return &api.OperationResp{Ok: true}, nil
}

func (f *fakeBackend) GetHistory(context.Context) (*api.HistoryResp, error) {
	return &api.HistoryResp{Ok: true, Entries: []*api.HistoryEntry{{ID: "1", Text: "hello", At: "now"}}}, nil
}

func (f *fakeBackend) DeleteHistory(context.Context, string) (*api.OperationResp, error) {
	return &api.OperationResp{Ok: true}, nil
}

func (f *fakeBackend) ClearHistory(context.Context) (*api.OperationResp, error) {
	return &api.OperationResp{Ok: true}, nil
}

func (f *fakeBackend) GetTextConfig(context.Context) (*api.TextConfigResp, error) {
	return &api.TextConfigResp{Ok: true, Config: &api.TextConfig{}}, nil
}

func (f *fakeBackend) UpdateTextConfig(context.Context, *api.TextConfig) (*api.OperationResp, error) {
	return &api.OperationResp{Ok: true}, nil
}
