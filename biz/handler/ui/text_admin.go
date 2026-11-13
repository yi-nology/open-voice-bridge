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

// PhrasePaste inserts a common phrase into the focused window on request —
// the click path behind the picker overlay.
func (s *Server) PhrasePaste(ctx context.Context, c *app.RequestContext) {
	if s.deps.PastePhrase == nil {
		c.JSON(consts.StatusServiceUnavailable, map[string]any{"ok": false, "error": "phrases unavailable"})
		return
	}
	var req struct {
		Slot int `json:"slot"`
	}
	if err := json.Unmarshal(c.Request.Body(), &req); err != nil || req.Slot < 1 {
		c.JSON(consts.StatusBadRequest, map[string]any{"ok": false, "error": "slot required"})
		return
	}
	text, ok := s.deps.PastePhrase(req.Slot)
	if !ok {
		c.JSON(consts.StatusOK, map[string]any{"ok": false, "error": "该槽位没有短语"})
		return
	}
	c.JSON(consts.StatusOK, map[string]any{"ok": true, "slot": req.Slot, "text": text})
}

// StreamingSubtitles returns the live-partial-recognition toggle (S6).
func (s *Server) StreamingSubtitles(ctx context.Context, c *app.RequestContext) {
	if s.deps.StreamingSubtitles == nil {
		c.JSON(consts.StatusServiceUnavailable, map[string]any{"ok": false, "error": "streaming unavailable"})
		return
	}
	c.JSON(consts.StatusOK, map[string]any{"ok": true, "enabled": s.deps.StreamingSubtitles()})
}

// StreamingSubtitlesSave stores the streaming toggle and applies it live.
func (s *Server) StreamingSubtitlesSave(ctx context.Context, c *app.RequestContext) {
	if s.deps.SetStreamingSubtitles == nil {
		c.JSON(consts.StatusServiceUnavailable, map[string]any{"ok": false, "error": "streaming unavailable"})
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(c.Request.Body(), &req); err != nil {
		c.JSON(consts.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if err := s.deps.SetStreamingSubtitles(req.Enabled); err != nil {
		c.JSON(consts.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(consts.StatusOK, map[string]any{"ok": true})
}
