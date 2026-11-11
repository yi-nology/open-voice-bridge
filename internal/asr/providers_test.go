package asr

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yi-nology/open-voice-bridge/internal/config"
)

func toneSamples(n int) []int16 {
	samples := make([]int16, n)
	for i := range samples {
		samples[i] = 1000
	}
	return samples
}

func TestMimoTranscribe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("api-key") != "test-key" {
			t.Errorf("api-key header = %q", r.Header.Get("api-key"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body["model"] != "mimo-v2.5-asr" {
			t.Errorf("model = %v", body["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, `{"choices":[{"message":{"content":"你好。"}}]}`); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	engine := &Mimo{
		APIKey:   "test-key",
		Model:    "mimo-v2.5-asr",
		Language: "auto",
		BaseURL:  server.URL,
		Client:   server.Client(),
	}
	result, err := engine.Transcribe(context.Background(), Audio{
		Samples:    toneSamples(16000),
		SampleRate: 16000,
	})
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if result.Text != "你好。" {
		t.Fatalf("text = %q", result.Text)
	}
}

func TestOpenAITranscribe(t *testing.T) {
	var gotModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
		}
		gotModel = r.FormValue("model")
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, `{"text":"你好"}`); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	engine, err := NewOpenAI(OpenAIConfig{
		BaseURL: server.URL + "/v1",
		Model:   "whisper-1",
	})
	if err != nil {
		t.Fatalf("new openai: %v", err)
	}
	result, err := engine.Transcribe(context.Background(), Audio{
		Samples:    toneSamples(16000),
		SampleRate: 16000,
	})
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if result.Text != "你好" {
		t.Fatalf("text = %q", result.Text)
	}
	if gotModel != "whisper-1" {
		t.Fatalf("model field = %q", gotModel)
	}
}

func TestOpenAILocalURL(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1:8080/v1",
		"http://localhost:8080/v1",
		"http://192.168.1.10:8080/v1",
	} {
		if !IsLocalURL(raw) {
			t.Errorf("expected local: %s", raw)
		}
	}
	if IsLocalURL("https://api.openai.com/v1") {
		t.Errorf("cloud URL reported local")
	}
}

func TestBuildFallsBackWhenPrimaryUnavailable(t *testing.T) {
	cfg := config.ASRConfig{
		Provider: "mimo",
		Fallback: []string{"local"},
		Providers: map[string]config.ASRProviderConfig{
			"mimo":  {Type: "mimo", APIKeyFile: filepath.Join(t.TempDir(), "missing")},
			"local": {Type: "command", Binary: "/bin/echo"},
		},
	}
	engine, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer engine.Close(context.Background())
	if !strings.Contains(engine.Name(), "echo") {
		t.Fatalf("engine name = %q, want fallback command", engine.Name())
	}
}

func TestCommandProviderTranscribes(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-asr.sh")
	body := "#!/bin/sh\ncase \"$1\" in\n  *.wav) echo 你好 ;;\nesac\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	command, err := NewCommand(CommandConfig{Binary: script, Args: []string{"{audio}"}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := command.Transcribe(context.Background(), Audio{Samples: toneSamples(8000), SampleRate: 16000})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if result.Text != "你好" {
		t.Fatalf("text = %q", result.Text)
	}
}

// TestProviderHTTPErrorOmitsBody pins the rule that a provider body never rides
// into an error: a rejected request commonly quotes the text that tripped the
// filter, and errors are logged (SEC-02). The status and the machine-readable
// codes stay, so a broken configuration is still explainable.
func TestProviderHTTPErrorOmitsBody(t *testing.T) {
	const secret = "你好世界"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","code":"content_filter","message":"rejected: ` + secret + `"}}`))
	}))
	defer server.Close()

	keyFile := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(keyFile, []byte("test-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	mimo, err := NewMimo(keyFile, "", "", server.URL)
	if err != nil {
		t.Fatalf("new mimo: %v", err)
	}
	openai, err := NewOpenAI(OpenAIConfig{BaseURL: server.URL + "/v1", Model: "whisper-1"})
	if err != nil {
		t.Fatalf("new openai: %v", err)
	}
	for name, engine := range map[string]Engine{"mimo": mimo, "openai": openai} {
		_, err := engine.Transcribe(context.Background(), Audio{Samples: toneSamples(16000), SampleRate: 16000})
		if err == nil {
			t.Fatalf("%s: want an error", name)
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("%s error echoes the response body: %v", name, err)
		}
		if strings.Contains(err.Error(), "rejected") {
			t.Fatalf("%s error carries provider prose: %v", name, err)
		}
		if !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "content_filter") {
			t.Fatalf("%s error lost the diagnosis: %v", name, err)
		}
	}
}

// TestCommandErrorOmitsStderr covers the same leak through a user-supplied ASR
// command, whose stderr can carry the recognised speech.
func TestCommandErrorOmitsStderr(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "loud-asr.sh")
	body := "#!/bin/sh\necho 'partial result: 你好世界' >&2\nexit 3\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	command, err := NewCommand(CommandConfig{Binary: script, Args: []string{"{audio}"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = command.Transcribe(context.Background(), Audio{Samples: toneSamples(8000), SampleRate: 16000})
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), "你好世界") {
		t.Fatalf("error carries stderr: %v", err)
	}
	if !strings.Contains(err.Error(), "exit status 3") || !strings.Contains(err.Error(), "bytes") {
		t.Fatalf("error lost the diagnosis: %v", err)
	}
}
