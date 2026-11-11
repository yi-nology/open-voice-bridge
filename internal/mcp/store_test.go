package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(filepath.Join(t.TempDir(), "mcp-tokens.json"))
}

func TestTokenCreateValidateRevoke(t *testing.T) {
	store := newTestStore(t)

	id, raw, err := store.Create("Claude Code")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id == "" || len(raw) < 64 {
		t.Fatalf("id=%q raw len=%d, want a 256-bit token", id, len(raw))
	}
	if strings.Contains(id, raw) {
		t.Fatal("the id must not embed the raw token")
	}

	info, ok := store.Validate(raw)
	if !ok || info.ID != id || info.Label != "Claude Code" {
		t.Fatalf("validate = %+v,%v", info, ok)
	}
	if _, ok := store.Validate(raw + "x"); ok {
		t.Fatal("garbage token accepted")
	}
	if _, ok := store.Validate(""); ok {
		t.Fatal("empty token accepted")
	}

	if err := store.Revoke(id); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, ok := store.Validate(raw); ok {
		t.Fatal("revoked token still validates")
	}
	if err := store.Revoke(id); err == nil {
		t.Fatal("double revoke should fail")
	}
}

func TestTokenListHidesSecrets(t *testing.T) {
	store := newTestStore(t)
	_, raw, err := store.Create("Codex")
	if err != nil {
		t.Fatal(err)
	}
	list := store.List()
	if len(list) != 1 || list[0].Label != "Codex" || list[0].Revoked {
		t.Fatalf("list = %+v", list)
	}
	// TokenInfo has no hash field on purpose: display records cannot leak
	// verifier material even in JSON.
	encoded := strings.TrimSpace(string(mustJSON(t, list)))
	if strings.Contains(encoded, raw) {
		t.Fatal("List leaked the raw token")
	}
}

func TestTokenStorePersistsAndSurvivesReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp-tokens.json")
	store := NewStore(path)
	id, raw, err := store.Create("Codex")
	if err != nil {
		t.Fatal(err)
	}
	reloaded := NewStore(path)
	if info, ok := reloaded.Validate(raw); !ok || info.ID != id {
		t.Fatalf("reload validate = %+v,%v", info, ok)
	}
	if err := reloaded.Revoke(id); err != nil {
		t.Fatal(err)
	}
	again := NewStore(path)
	if _, ok := again.Validate(raw); ok {
		t.Fatal("revocation did not survive reload")
	}
}

// A revoked token must stop working immediately, not at the next restart
// (SayAll 撤销即失效).
func TestValidateRefreshesLastUsed(t *testing.T) {
	store := newTestStore(t)
	clock := time.Unix(1_700_000_000, 0)
	store.now = func() time.Time { return clock }
	_, raw, err := store.Create("open-code")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Validate(raw); !ok {
		t.Fatal("fresh token rejected")
	}
	clock = clock.Add(time.Hour)
	if _, ok := store.Validate(raw); !ok {
		t.Fatal("second validate rejected")
	}
	list := store.List()
	if !list[0].LastUsed.Equal(clock) {
		t.Fatalf("last_used = %v, want %v", list[0].LastUsed, clock)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// The audit trail records who asked for what and how much came back — never
// the speech itself.
func TestAuditRecordsCountsNotContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp-audit.jsonl")
	if err := AppendAudit(path, AuditEntry{
		At:      time.Unix(1_700_000_000, 0),
		TokenID: "mcp-1",
		Label:   "Claude Code",
		Tool:    "history_list",
		Items:   3,
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, `"tool":"history_list"`) || !strings.Contains(body, `"items":3`) {
		t.Fatalf("audit = %s", body)
	}
	if strings.Contains(body, "text") || strings.Contains(body, "query") {
		t.Fatalf("audit must not carry content fields: %s", body)
	}
}

func TestClientConfigSnippet(t *testing.T) {
	snippet := ClientConfig("xiaomi-remote", "/usr/local/bin/xiaomi-remote", "tok-123")
	servers, _ := snippet["mcpServers"].(map[string]any)
	entry, _ := servers["xiaomi-remote"].(map[string]any)
	if entry["command"] != "/usr/local/bin/xiaomi-remote" {
		t.Fatalf("command = %v", entry["command"])
	}
	args, _ := entry["args"].([]string)
	if len(args) != 3 || args[0] != "mcp" || args[2] != "tok-123" {
		t.Fatalf("args = %v", args)
	}
}
