package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	oeerrors "github.com/oekit/pkg/errors"

	"github.com/yi-nology/open-voice-bridge/internal/mcp"
)

func (s *Service) mcpPath(name string) string {
	return filepath.Join(filepath.Dir(s.path), name)
}

// MCPCreateToken mints one client grant. The raw token is returned exactly
// once, together with the stdio snippet to paste into the client's config.
func (s *Service) MCPCreateToken(label, binary string) (map[string]any, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return nil, oeerrors.New("label is required")
	}
	id, raw, err := s.mcpTokens.Create(label)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":      true,
		"id":      id,
		"label":   label,
		"token":   raw,
		"snippet": mcp.ClientConfig("xiaomi-remote", binary, raw),
	}, nil
}

// MCPListTokens shows the grants (never verifier material).
func (s *Service) MCPListTokens() map[string]any {
	return map[string]any{"ok": true, "tokens": s.mcpTokens.List()}
}

// MCPRevokeToken retires a grant; running sessions fail on their next call.
func (s *Service) MCPRevokeToken(id string) map[string]any {
	if err := s.mcpTokens.Revoke(id); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true, "disconnected": true}
}

// MCPVerify checks a client token for the read-only agent endpoints.
func (s *Service) MCPVerify(raw string) (mcp.TokenInfo, bool) {
	return s.mcpTokens.Validate(raw)
}

// MCPAudit records one agent query. Counts only: never speech content and
// never the search text (SayAll 按天审计).
func (s *Service) MCPAudit(info mcp.TokenInfo, tool string, items int) {
	entry := mcp.AuditEntry{
		At:      time.Now(),
		TokenID: info.ID,
		Label:   info.Label,
		Tool:    tool,
		Items:   items,
	}
	if err := mcp.AppendAudit(s.mcpPath("mcp-audit.jsonl"), entry); err != nil {
		s.logger.Warn("append mcp audit", "err", err)
	}
}

// MCPAuditBody renders the audit trail for the admin panel (counts only).
func (s *Service) MCPAuditBody() map[string]any {
	data, err := os.ReadFile(s.mcpPath("mcp-audit.jsonl"))
	if err != nil {
		return map[string]any{"ok": true, "entries": []any{}}
	}
	entries := []any{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry mcp.AuditEntry
		if err := json.Unmarshal([]byte(line), &entry); err == nil {
			entries = append(entries, entry)
		}
	}
	return map[string]any{"ok": true, "entries": entries}
}
