package mcp

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/yi-nology/open-voice-bridge/internal/core"
	"github.com/yi-nology/open-voice-bridge/internal/fsutil"
)

// AuditEntry records one agent query for the local audit trail ("按天审计").
// It carries counts only — never speech content, never the query text.
// The type lives in core (core 类型迁移).
type AuditEntry = core.AuditEntry

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
