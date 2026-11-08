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
	cfg := Config{
		Provider: "mimo",
		Fallback: []string{"local"},
		Providers: map[string]ProviderConfig{
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
