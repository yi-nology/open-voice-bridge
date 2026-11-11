package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func rpc(id any, method string, params any) string {
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	if id != nil {
		msg["id"] = id
	}
	if params != nil {
		msg["params"] = params
	}
	data, _ := json.Marshal(msg)
	return string(data) + "\n"
}

func runSession(t *testing.T, srv *Server, lines ...string) []map[string]any {
	t.Helper()
	input := strings.Join(lines, "")
	var output bytes.Buffer
	if err := srv.Serve(context.Background(), bufio.NewReader(strings.NewReader(input)), &output); err != nil {
		t.Fatalf("serve: %v", err)
	}
	var out []map[string]any
	scanner := bufio.NewScanner(&output)
	for scanner.Scan() {
		var msg map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			t.Fatalf("decode %q: %v", scanner.Text(), err)
		}
		out = append(out, msg)
	}
	return out
}

func fakeBackend(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/agent/verify", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "label": "Claude Code"})
	})
	mux.HandleFunc("/api/agent/history", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "entries": []any{
			map[string]any{"text": "你好世界", "at": "2026-11-11T10:00:00Z", "provider": "mimo"},
		}})
	})
	mux.HandleFunc("/api/agent/stats", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "utterances": 7, "characters": 91})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// The MCP handshake must speak the stdio protocol: initialize, tools/list,
// tools/call. Tools are read-only (SayAll 只读 MCP).
func TestMCPSessionListsAndCallsTools(t *testing.T) {
	backend := fakeBackend(t)
	srv := NewServer(backend.URL, "good-token", http.DefaultClient)

	out := runSession(t, srv,
		rpc(1, "initialize", map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{}}),
		rpc(nil, "notifications/initialized", nil),
		rpc(2, "tools/list", nil),
		rpc(3, "tools/call", map[string]any{"name": "history_list", "arguments": map[string]any{"limit": 5}}),
		rpc(4, "tools/call", map[string]any{"name": "usage_stats", "arguments": map[string]any{}}),
	)

	if len(out) != 4 {
		t.Fatalf("responses = %d, want 4 (a notification gets no reply)", len(out))
	}
	init := out[0]
	result, _ := init["result"].(map[string]any)
	if result["protocolVersion"] != "2024-11-05" {
		t.Fatalf("initialize result = %v", result)
	}
	tools, _ := out[1]["result"].(map[string]any)["tools"].([]any)
	names := map[string]bool{}
	for _, item := range tools {
		tool, _ := item.(map[string]any)
		names[tool["name"].(string)] = true
	}
	for _, want := range []string{"history_list", "history_search", "usage_stats"} {
		if !names[want] {
			t.Fatalf("tools/list missing %q: %v", want, names)
		}
	}
	call, _ := out[2]["result"].(map[string]any)
	content, _ := call["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("history_list content = %v", call)
	}
	text, _ := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "你好世界") {
		t.Fatalf("history text = %q", text)
	}
	stats, _ := out[3]["result"].(map[string]any)
	statsContent, _ := stats["content"].([]any)
	statsText, _ := statsContent[0].(map[string]any)["text"].(string)
	if !strings.Contains(statsText, "utterances") {
		t.Fatalf("stats text = %q", statsText)
	}
}

// A revoked or unknown token must not serve any tool, even mid-session.
func TestMCPRefusesInvalidToken(t *testing.T) {
	backend := fakeBackend(t)
	srv := NewServer(backend.URL, "bad-token", http.DefaultClient)
	out := runSession(t, srv,
		rpc(1, "tools/call", map[string]any{"name": "history_list", "arguments": map[string]any{}}),
	)
	if len(out) != 1 {
		t.Fatalf("responses = %v", out)
	}
	result, _ := out[0]["result"].(map[string]any)
	if result == nil {
		t.Fatalf("want an error result, got %v", out[0])
	}
	if result["isError"] != true {
		t.Fatalf("unauthorised call must report isError, got %v", result)
	}
}

func TestMCPUnknownMethodAndTool(t *testing.T) {
	backend := fakeBackend(t)
	srv := NewServer(backend.URL, "good-token", http.DefaultClient)
	out := runSession(t, srv,
		rpc(1, "bogus/method", nil),
		rpc(2, "tools/call", map[string]any{"name": "history_delete", "arguments": map[string]any{}}),
	)
	if out[0]["error"] == nil {
		t.Fatalf("unknown method must return an error, got %v", out[0])
	}
	result, _ := out[1]["result"].(map[string]any)
	if result["isError"] != true {
		t.Fatalf("unknown tool must report isError, got %v", out[1])
	}
}
