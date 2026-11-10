package ui

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// ConfigExport serves the configuration for a backup. Key material is never
// included: providers reference key files by path only.
func (s *Server) ConfigExport(ctx context.Context, c *app.RequestContext) {
	if s.deps.ExportConfig == nil {
		c.JSON(consts.StatusServiceUnavailable, map[string]any{"ok": false, "error": "export unavailable"})
		return
	}
	data, err := s.deps.ExportConfig()
	if err != nil {
		c.JSON(consts.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	c.Header("Content-Disposition", `attachment; filename="xiaomi-remote-config.json"`)
	c.Data(consts.StatusOK, "application/json; charset=utf-8", data)
}

// ConfigImport validates an exported config and swaps it in. The previous
// file is kept as a backup; a rejected document changes nothing.
func (s *Server) ConfigImport(ctx context.Context, c *app.RequestContext) {
	if s.deps.ImportConfig == nil {
		c.JSON(consts.StatusServiceUnavailable, map[string]any{"ok": false, "error": "import unavailable"})
		return
	}
	if err := s.deps.ImportConfig(c.Request.Body()); err != nil {
		c.JSON(consts.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(consts.StatusOK, map[string]any{"ok": true, "restart_required": true})
}

// Diagnostics returns the redacted troubleshooting summary to attach to an
// issue: no device addresses, no home paths, no speech.
func (s *Server) Diagnostics(ctx context.Context, c *app.RequestContext) {
	if s.deps.Diagnostics == nil {
		c.JSON(consts.StatusServiceUnavailable, map[string]any{"ok": false, "error": "diagnostics unavailable"})
		return
	}
	c.JSON(consts.StatusOK, s.deps.Diagnostics(ctx))
}
