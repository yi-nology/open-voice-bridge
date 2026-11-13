package ui

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/yi-nology/open-voice-bridge/internal/config"
)

// llmAppRulesResp is the typed envelope for both app-rules endpoints: the
// ARC-11 map ratchet counts every string-keyed any-map in Go sources, so
// ad-hoc response shapes go in a struct instead (the JSON on the wire is
// unchanged).
type llmAppRulesResp struct {
	OK    bool                    `json:"ok"`
	Error string                  `json:"error,omitempty"`
	Rules []config.AppRewriteRule `json:"rules,omitempty"`
}

// LLMAppRules returns the app-aware rewrite rules (S5). They ride a free-form
// endpoint beside the generated LLM config model.
func (s *Server) LLMAppRules(ctx context.Context, c *app.RequestContext) {
	if s.deps.LLMAppRules == nil {
		c.JSON(consts.StatusServiceUnavailable, llmAppRulesResp{Error: "app rules unavailable"})
		return
	}
	c.JSON(consts.StatusOK, llmAppRulesResp{OK: true, Rules: s.deps.LLMAppRules()})
}

// LLMAppRulesSave stores the app-aware rewrite rules and applies them live.
func (s *Server) LLMAppRulesSave(ctx context.Context, c *app.RequestContext) {
	if s.deps.UpdateLLMAppRules == nil {
		c.JSON(consts.StatusServiceUnavailable, llmAppRulesResp{Error: "app rules unavailable"})
		return
	}
	var req struct {
		Rules []config.AppRewriteRule `json:"rules"`
	}
	if err := json.Unmarshal(c.Request.Body(), &req); err != nil {
		c.JSON(consts.StatusBadRequest, llmAppRulesResp{Error: err.Error()})
		return
	}
	if err := s.deps.UpdateLLMAppRules(req.Rules); err != nil {
		c.JSON(consts.StatusBadRequest, llmAppRulesResp{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, llmAppRulesResp{OK: true})
}
