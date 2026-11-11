package mcp

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/fsutil"
)

// AuditEntry records one agent query for the local audit trail ("按天审计").
// It carries counts only — never speech content, never the query text.
type AuditEntry struct {
	At      time.Time `json:"at"`
	TokenID string    `json:"token_id"`
	Label   string    `json:"label"`
	Tool    string    `json:"tool"`
	Items   int       `json:"items"`
}

// AppendAudit adds one line to the 0600 audit log.
func AppendAudit(path string, entry AuditEntry) error {
	if path == "" {
		return nil
	}
	auditMu.Lock()
	defer auditMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return fsutil.AppendJSONL(path, entry)
}

// auditMu serialises appends from concurrent query handlers.
var auditMu sync.Mutex

// ClientConfig renders the snippet an AI client needs to launch this server:
// a stdio MCP entry with the user's token. The token is shown to the user once
// and lives only in their client config.
func ClientConfig(label, binary, token string) map[string]any {
	if label == "" {
		label = "xiaomi-remote"
	}
	return map[string]any{
		"mcpServers": map[string]any{
			label: map[string]any{
				"command": binary,
				"args":    []string{"mcp", "--token", token},
			},
		},
	}
}
