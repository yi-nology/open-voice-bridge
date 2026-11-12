package ui

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// TextCommands returns the voice-command toggle and map (语音命令). The typed
// text-config model is generated and cannot carry them, so they ride this
// free-form endpoint beside it.
func (s *Server) TextCommands(ctx context.Context, c *app.RequestContext) {
	if s.deps.TextCommands == nil {
		c.JSON(consts.StatusServiceUnavailable, map[string]any{"ok": false, "error": "commands unavailable"})
		return
	}
	enabled, commands := s.deps.TextCommands()
	c.JSON(consts.StatusOK, map[string]any{"ok": true, "enabled": enabled, "commands": commands})
}

// TextCommandsSave stores the voice-command toggle and map and applies them
// to the live rules immediately.
func (s *Server) TextCommandsSave(ctx context.Context, c *app.RequestContext) {
	if s.deps.UpdateTextCommands == nil {
		c.JSON(consts.StatusServiceUnavailable, map[string]any{"ok": false, "error": "commands unavailable"})
		return
	}
	var req struct {
		Enabled  bool              `json:"enabled"`
		Commands map[string]string `json:"commands"`
	}
	if err := json.Unmarshal(c.Request.Body(), &req); err != nil {
		c.JSON(consts.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if err := s.deps.UpdateTextCommands(req.Enabled, req.Commands); err != nil {
		c.JSON(consts.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(consts.StatusOK, map[string]any{"ok": true})
}
