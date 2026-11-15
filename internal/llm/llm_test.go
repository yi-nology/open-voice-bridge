package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/yi-nology/open-voice-bridge/internal/config"
)

func TestBuildDisabled(t *testing.T) {
	processor, err := Build(config.LLMConfig{Enabled: false})
	if err != nil || processor != nil {
		t.Fatalf("processor=%v err=%v", processor, err)
	}
}

func TestPresets(t *testing.T) {
	for _, id := range []string{"minimax", "volcengine", "openai", "deepseek", "custom"} {
		if _, ok := config.LLMPresetByID(id); !ok {
			t.Fatalf("missing preset %q", id)
		}
	}
}

func TestOpenAIProcess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["model"] != "test-model" {
			t.Fatalf("model = %v", body["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"你好，世界。"}}]}`))
	}))
	defer server.Close()
	keyFile := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(keyFile, []byte("test-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	processor, err := Build(config.LLMConfig{
		Enabled:    true,
		Provider:   "custom",
		BaseURL:    server.URL + "/v1",
		Model:      "test-model",
		APIKeyFile: keyFile,
		Mode:       "cleanup",
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	result, err := processor.Process(context.Background(), "嗯 你好")
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if result != "你好，世界。" {
		t.Fatalf("result = %q", result)
	}
}

func TestOpenAIHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()
	processor, err := NewOpenAI(OpenAIConfig{BaseURL: server.URL, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = processor.Process(context.Background(), "hello")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v", err)
	}
}

// TestOpenAIHTTPErrorOmitsBody pins the rule that a provider body never rides
// into an error: a rejected request commonly quotes the text that tripped the
// filter, and errors are logged by the caller (SEC-02).
func TestOpenAIHTTPErrorOmitsBody(t *testing.T) {
	const secret = "你好世界"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","code":"content_filter","message":"rejected: ` + secret + `"}}`))
	}))
	defer server.Close()
	processor, err := NewOpenAI(OpenAIConfig{BaseURL: server.URL, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = processor.Process(context.Background(), secret)
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "rejected") {
		t.Fatalf("error carries provider prose: %v", err)
	}
	if !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "content_filter") {
		t.Fatalf("error lost the diagnosis: %v", err)
	}
}

func TestOpenAIPing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}]}`))
	}))
	defer server.Close()
	processor, err := NewOpenAI(OpenAIConfig{BaseURL: server.URL, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	latency, err := processor.Ping(context.Background())
	if err != nil || latency <= 0 {
		t.Fatalf("latency=%v err=%v", latency, err)
	}
}

// TestProcessModeConcurrentKeepsEachMode pins the S5 concurrency contract:
// ProcessMode must not mutate shared state — two callers with different modes
// may run at the same time and each request has to carry its own system prompt.
// The body's user text names the mode the call asked for (P1 fix).
func TestProcessModeConcurrentKeepsEachMode(t *testing.T) {
	type call struct {
		text string
		mode string
	}
	want := map[string]string{}
	for _, mode := range []string{"format", "translate", "cleanup"} {
		want[mode] = systemPrompt(mode, "")
	}
	var mu sync.Mutex
	got := map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		sys, user := "", ""
		for _, m := range body.Messages {
			switch m.Role {
			case "system":
				sys = m.Content
			case "user":
				user = m.Content
			}
		}
		mu.Lock()
		got[user] = sys
		mu.Unlock()
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	processor, err := NewOpenAI(OpenAIConfig{BaseURL: server.URL, Model: "m", Mode: "cleanup"})
	if err != nil {
		t.Fatal(err)
	}
	calls := []call{{"format", "format"}, {"translate", "translate"}, {"cleanup", "cleanup"}}
	var wg sync.WaitGroup
	for _, c := range calls {
		wg.Add(1)
		go func(c call) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				if _, err := processor.ProcessMode(context.Background(), c.text, c.mode); err != nil {
					t.Error(err)
					return
				}
			}
		}(c)
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	for _, c := range calls {
		if got[c.text] != want[c.mode] {
			t.Fatalf("request for %q carried system prompt of another mode:\n got %q\nwant %q",
				c.text, got[c.text], want[c.mode])
		}
	}
}
