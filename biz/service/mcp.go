package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	oeerrors "github.com/oekit/pkg/errors"

	"github.com/yi-nology/open-voice-bridge/internal/core"
)

func (s *Service) mcpPath(name string) string {
	return filepath.Join(filepath.Dir(s.path), name)
}

// MCPCreateToken mints one client grant. The raw token is returned exactly
// once, together with the stdio snippet to paste into the client's config.
func (s *Service) MCPCreateToken(label, binary string) (core.MCPTokenView, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return core.MCPTokenView{}, oeerrors.New("label is required")
	}
	id, raw, err := s.mcpTokens.Create(label)
	if err != nil {
		return core.MCPTokenView{}, err
	}
	return core.MCPTokenView{
		OK:      true,
		ID:      id,
		Label:   label,
		Token:   raw,
		Snippet: core.ClientConfigSnippet("xiaoyao", binary, raw),
	}, nil
}

// MCPListTokens shows the grants (never verifier material).
func (s *Service) MCPListTokens() core.MCPTokenListView {
	return core.MCPTokenListView{OK: true, Tokens: s.mcpTokens.List()}
}

// MCPRevokeToken retires a grant; running sessions fail on their next call.
func (s *Service) MCPRevokeToken(id string) core.MCPTokenRevokedView {
	if err := s.mcpTokens.Revoke(id); err != nil {
		return core.MCPTokenRevokedView{Error: err.Error()}
	}
	return core.MCPTokenRevokedView{OK: true, Disconnected: true}
}

// MCPVerify checks a client token for the read-only agent endpoints.
func (s *Service) MCPVerify(raw string) (core.TokenInfo, bool) {
	return s.mcpTokens.Validate(raw)
}

// MCPAudit records one agent query. Counts only: never speech content and
// never the search text (SayAll 按天审计).
func (s *Service) MCPAudit(info core.TokenInfo, tool string, items int) {
	entry := core.AuditEntry{
		At:      time.Now(),
		TokenID: info.ID,
		Label:   info.Label,
		Tool:    tool,
		Items:   items,
	}
	if err := s.ops.AppendAudit(entry); err != nil {
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
		var entry core.AuditEntry
		if err := json.Unmarshal([]byte(line), &entry); err == nil {
			entries = append(entries, entry)
		}
	}
	return map[string]any{"ok": true, "entries": entries}
}
