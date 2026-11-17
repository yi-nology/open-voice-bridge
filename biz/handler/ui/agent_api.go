package ui

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/yi-nology/open-voice-bridge/internal/core"
	"github.com/yi-nology/open-voice-bridge/internal/history"
	"github.com/yi-nology/open-voice-bridge/internal/redact"
)

func bearerToken(c *app.RequestContext) string {
	auth := strings.TrimSpace(string(c.Request.Header.Peek("Authorization")))
	if len(auth) > 7 && strings.EqualFold(auth[:7], "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return ""
}

// AgentVerify tells a fresh MCP session whose grant it is using.
func (s *Server) AgentVerify(ctx context.Context, c *app.RequestContext) {
	info, ok := s.deps.MCPVerify(bearerToken(c))
	if !ok {
		c.JSON(consts.StatusUnauthorized, reply{Error: "unknown or revoked token"})
		return
	}
	c.JSON(consts.StatusOK, core.AgentIdentityView{OK: true, ID: info.ID, Label: info.Label})
}

// AgentHistory serves dictation history to an authorised AI client. Device
// addresses are redacted exactly like the diagnostics summary; the audit
// records counts only.
func (s *Server) AgentHistory(ctx context.Context, c *app.RequestContext) {
	info, ok := s.deps.MCPVerify(bearerToken(c))
	if !ok {
		c.JSON(consts.StatusUnauthorized, reply{Error: "unknown or revoked token"})
		return
	}
	limit := queryInt(c, "limit", 20, 200)
	query := strings.TrimSpace(string(c.Query("q")))
	entries := s.deps.History(ctx)
	out := make([]core.AgentEntryView, 0, limit)
	for _, entry := range entries {
		if query != "" && !strings.Contains(strings.ToLower(entry.Text), strings.ToLower(query)) {
			continue
		}
		out = append(out, publicEntry(entry))
		if len(out) >= limit {
			break
		}
	}
	tool := "history_list"
	if query != "" {
		tool = "history_search"
	}
	s.deps.MCPAudit(info, tool, len(out))
	c.JSON(consts.StatusOK, core.AgentHistoryView{OK: true, Entries: out})
}

// AgentStats serves the cumulative counters (counts only, never content).
func (s *Server) AgentStats(ctx context.Context, c *app.RequestContext) {
	info, ok := s.deps.MCPVerify(bearerToken(c))
	if !ok {
		c.JSON(consts.StatusUnauthorized, reply{Error: "unknown or revoked token"})
		return
	}
	snap := s.deps.Stats()
	s.deps.MCPAudit(info, "usage_stats", 0)
	c.JSON(consts.StatusOK, history.StatsView{OK: true, UsageStats: snap})
}

// publicEntry is the client-facing shape: the spoken text and its metadata,
// with the device address redacted (SEC-02).
func publicEntry(entry history.Entry) core.AgentEntryView {
	return core.AgentEntryView{
		Text:      entry.Text,
		At:        entry.At,
		Provider:  entry.Provider,
		Language:  entry.Language,
		Processed: entry.Processed,
		Address:   redact.MAC(entry.Address),
	}
}

// MCPTokens lists the current grants (never verifier material).
func (s *Server) MCPTokens(ctx context.Context, c *app.RequestContext) {
	c.JSON(consts.StatusOK, s.deps.MCPListTokens())
}

// MCPCreateToken mints a grant; the raw token is in the response exactly once.
func (s *Server) MCPCreateToken(ctx context.Context, c *app.RequestContext) {
	var body struct {
		Label string `json:"label"`
	}
	if err := json.Unmarshal(c.Request.Body(), &body); err != nil {
		c.JSON(consts.StatusBadRequest, reply{Error: "bad json"})
		return
	}
	resp, err := s.deps.MCPCreateToken(body.Label)
	if err != nil {
		c.JSON(consts.StatusBadRequest, reply{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, resp)
}

// MCPRevokeToken retires a grant immediately.
func (s *Server) MCPRevokeToken(ctx context.Context, c *app.RequestContext) {
	var body struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(c.Request.Body(), &body); err != nil || strings.TrimSpace(body.ID) == "" {
		c.JSON(consts.StatusBadRequest, reply{Error: "id is required"})
		return
	}
	c.JSON(consts.StatusOK, s.deps.MCPRevokeToken(body.ID))
}

func queryInt(c *app.RequestContext, key string, fallback, max int) int {
	value := 0
	for _, ch := range string(c.Query(key)) {
		if ch < '0' || ch > '9' {
			return fallback
		}
		value = value*10 + int(ch-'0')
		if value > max {
			return max
		}
	}
	if value == 0 {
		return fallback
	}
	return value
}
