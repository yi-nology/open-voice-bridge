package ui

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/yi-nology/open-voice-bridge/internal/config"
)

// LLMAppRules returns the app-aware rewrite rules (S5). They ride a free-form
// endpoint beside the generated LLM config model.
func (s *Server) LLMAppRules(ctx context.Context, c *app.RequestContext) {
	if s.deps.LLMAppRules == nil {
		c.JSON(consts.StatusServiceUnavailable, map[string]any{"ok": false, "error": "app rules unavailable"})
		return
	}
	c.JSON(consts.StatusOK, map[string]any{"ok": true, "rules": s.deps.LLMAppRules()})
}

// LLMAppRulesSave stores the app-aware rewrite rules and applies them live.
func (s *Server) LLMAppRulesSave(ctx context.Context, c *app.RequestContext) {
	if s.deps.UpdateLLMAppRules == nil {
		c.JSON(consts.StatusServiceUnavailable, map[string]any{"ok": false, "error": "app rules unavailable"})
		return
	}
	var req struct {
		Rules []config.AppRewriteRule `json:"rules"`
	}
	if err := json.Unmarshal(c.Request.Body(), &req); err != nil {
		c.JSON(consts.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if err := s.deps.UpdateLLMAppRules(req.Rules); err != nil {
		c.JSON(consts.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(consts.StatusOK, map[string]any{"ok": true})
}
