package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/yi-nology/open-voice-bridge/internal/history"
	"github.com/yi-nology/open-voice-bridge/internal/mcp"
)

func agentEngine(deps Deps) *server.Hertz {
	s := New(deps)
	h := server.New()
	h.Use(s.Security)
	h.GET("/api/agent/verify", s.AgentVerify)
	h.GET("/api/agent/history", s.AgentHistory)
	h.GET("/api/agent/stats", s.AgentStats)
	h.GET("/api/mcp/tokens", s.MCPTokens)
	h.POST("/api/mcp/tokens", s.MCPCreateToken)
	h.POST("/api/mcp/tokens/revoke", s.MCPRevokeToken)
	return h
}

func agentDeps(audited *[]string) Deps {
	return Deps{
		MCPVerify: func(raw string) (mcp.TokenInfo, bool) {
			if raw != "good-token" {
				return mcp.TokenInfo{}, false
			}
			return mcp.TokenInfo{ID: "mcp-1", Label: "Claude Code"}, true
		},
		MCPAudit: func(info mcp.TokenInfo, tool string, items int) {
			*audited = append(*audited, tool)
		},
		History: func(context.Context) []history.Entry {
			return []history.Entry{
				{Text: "你好世界", At: time.Unix(1_700_000_000, 0), Address: "AA:BB:CC:DD:EE:FF", Provider: "mimo"},
				{Text: "明天天气", At: time.Unix(1_700_000_100, 0), Address: "AA:BB:CC:DD:EE:FF", Provider: "mimo"},
			}
		},
		Stats: func() history.UsageStats {
			return history.UsageStats{Utterances: 7, Characters: 91}
		},
	}
}

// The agent surface is token-gated: no bearer, no history.
func TestAgentHistoryRequiresToken(t *testing.T) {
	var audited []string
	h := agentEngine(agentDeps(&audited))
	resp := ut.PerformRequest(h.Engine, "GET", "http://127.0.0.1:8787/api/agent/history", nil)
	if resp.Code != consts.StatusUnauthorized {
		t.Fatalf("no bearer code = %d, want 401", resp.Code)
	}
	resp = ut.PerformRequest(h.Engine, "GET", "http://127.0.0.1:8787/api/agent/history", nil,
		ut.Header{Key: "Authorization", Value: "Bearer wrong"})
	if resp.Code != consts.StatusUnauthorized {
		t.Fatalf("bad bearer code = %d, want 401", resp.Code)
	}
	if len(audited) != 0 {
		t.Fatalf("failed calls must not audit: %v", audited)
	}
}

// History for AI clients carries the text but redacts device addresses, and
// every query leaves an audit record (counts only).
func TestAgentHistoryRedactsAddressAndAudits(t *testing.T) {
	var audited []string
	h := agentEngine(agentDeps(&audited))
	resp := ut.PerformRequest(h.Engine, "GET",
		"http://127.0.0.1:8787/api/agent/history?q=%E5%A4%A9%E6%B0%94&limit=1", nil,
		ut.Header{Key: "Authorization", Value: "Bearer good-token"})
	if resp.Code != consts.StatusOK {
		t.Fatalf("code = %d body = %s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if !strings.Contains(body, "明天天气") {
		t.Fatalf("search missed the entry: %s", body)
	}
	if strings.Contains(body, "你好世界") {
		t.Fatalf("limit/query not applied: %s", body)
	}
	if strings.Contains(body, "AA:BB:CC:DD:EE:FF") {
		t.Fatalf("agent history leaked the device address: %s", body)
	}
	if !strings.Contains(body, "AA:BB:CC:**:**:FF") {
		t.Fatalf("agent history should carry the redacted address: %s", body)
	}
	if len(audited) != 1 || audited[0] != "history_search" {
		t.Fatalf("audit = %v, want one history_search record", audited)
	}
}

func TestMCPCreateAndRevokeTokenFlow(t *testing.T) {
	var created, revoked string
	h := agentEngine(Deps{
		MCPCreateToken: func(label string) (map[string]any, error) {
			created = label
			return map[string]any{"ok": true, "id": "mcp-9", "label": label, "token": "raw-once",
				"snippet": map[string]any{"mcpServers": map[string]any{}}}, nil
		},
		MCPListTokens: func() map[string]any {
			return map[string]any{"ok": true, "tokens": []any{}}
		},
		MCPRevokeToken: func(id string) map[string]any {
			revoked = id
			return map[string]any{"ok": true}
		},
	})
	createBody := `{"label":"Claude Code"}`
	resp := ut.PerformRequest(h.Engine, "POST", "http://127.0.0.1:8787/api/mcp/tokens",
		&ut.Body{Body: strings.NewReader(createBody), Len: len(createBody)},
		ut.Header{Key: "Origin", Value: "http://127.0.0.1:8787"},
		ut.Header{Key: "Sec-Fetch-Site", Value: "same-origin"})
	if resp.Code != consts.StatusOK || created != "Claude Code" {
		t.Fatalf("create code = %d label = %q body = %s", resp.Code, created, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "raw-once") {
		t.Fatalf("create response must carry the raw token once: %s", resp.Body.String())
	}
	revokeBody := `{"id":"mcp-9"}`
	resp = ut.PerformRequest(h.Engine, "POST", "http://127.0.0.1:8787/api/mcp/tokens/revoke",
		&ut.Body{Body: strings.NewReader(revokeBody), Len: len(revokeBody)},
		ut.Header{Key: "Origin", Value: "http://127.0.0.1:8787"},
		ut.Header{Key: "Sec-Fetch-Site", Value: "same-origin"})
	if resp.Code != consts.StatusOK || revoked != "mcp-9" {
		t.Fatalf("revoke code = %d id = %q", resp.Code, revoked)
	}
}
