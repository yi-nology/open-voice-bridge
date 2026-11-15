// Package router: handwritten route faces live next to the generated table
// so both the root binary and tests can compose the full route set (OPS-13).
package router

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	hertzadaptor "github.com/oekit/hertz/adaptor"

	"github.com/yi-nology/open-voice-bridge/biz/handler/ui"
)

// CustomizedRegister registers non-IDL routes: the embedded UI, assets, and SSE.
// The four route groups here are tracked in idl/handwritten-routes.txt and the
// hand-maintained section of docs/openapi.yaml (OPS-07).
func CustomizedRegister(r *server.Hertz, customUI *ui.Server) {
	if customUI == nil {
		return
	}
	r.GET("/", customUI.Root)
	r.GET("/ui.css", customUI.UIAsset("ui.css", "text/css; charset=utf-8"))
	r.GET("/ui.js", customUI.UIAsset("ui.js", "text/javascript; charset=utf-8"))
	r.GET("/ui-core.js", customUI.UIAsset("ui-core.js", "text/javascript; charset=utf-8"))
	r.GET("/tokens.css", customUI.UIAsset("tokens.css", "text/css; charset=utf-8"))
	assets := hertzadaptor.NativeStaticFSHandler("/assets", ui.AssetsFS(),
		hertzadaptor.WithNativeCacheControl("max-age=3600"))
	r.GET("/assets/*filepath", assets)
	r.HEAD("/assets/*filepath", assets)
	r.GET("/events", customUI.Events)
	r.GET("/api/stats", customUI.Stats)
	r.GET("/api/config/export", customUI.ConfigExport)
	r.POST("/api/config/import", customUI.ConfigImport)
	r.GET("/api/diagnostics", customUI.Diagnostics)
	r.GET("/api/permissions", customUI.Permissions)
	r.GET("/api/agent/verify", customUI.AgentVerify)
	r.GET("/api/agent/history", customUI.AgentHistory)
	r.GET("/api/agent/stats", customUI.AgentStats)
	r.GET("/api/mcp/tokens", customUI.MCPTokens)
	r.POST("/api/mcp/tokens", customUI.MCPCreateToken)
	r.POST("/api/mcp/tokens/revoke", customUI.MCPRevokeToken)

	// Management-plane phone/input endpoints (loopback + same-site only).
	r.GET("/api/text/commands", customUI.TextCommands)
	r.POST("/api/text/commands", customUI.TextCommandsSave)
	r.POST("/api/phrases/paste", customUI.PhrasePaste)
	r.GET("/api/llm/app-rules", customUI.LLMAppRules)
	r.POST("/api/llm/app-rules", customUI.LLMAppRulesSave)
	r.GET("/api/streaming", customUI.StreamingSubtitles)
	r.POST("/api/streaming", customUI.StreamingSubtitlesSave)
	r.GET("/api/paste", customUI.PasteConfig)
	r.POST("/api/paste", customUI.PasteConfigSave)
	r.GET("/api/wake", customUI.WakeConfig)
	r.POST("/api/wake", customUI.WakeConfigSave)
	r.GET("/api/lookback", customUI.LookbackConfig)
	r.POST("/api/lookback", customUI.LookbackConfigSave)
	r.POST("/api/lookback/recover", customUI.LookbackRecover)
	r.GET("/api/history/policy", customUI.HistoryPolicy)
	r.POST("/api/history/policy", customUI.HistoryPolicySave)
	r.GET("/api/inputs", customUI.Inputs)
	r.POST("/api/phone/enabled", customUI.PhoneEnabled)
	r.GET("/api/phone/power", customUI.PhonePowerKey)
	r.POST("/api/phone/power", customUI.PhonePowerKeySave)
	r.POST("/api/phone/pairing", customUI.PhonePairing)
	r.GET("/api/phone/devices", customUI.PhoneDevices)
	r.POST("/api/phone/revoke", customUI.PhoneRevoke)
}

// Register is the full route set: the generated IDL table plus the
// handwritten faces — what router_gen.go composes for the binary and what
// tests drive.
func Register(r *server.Hertz, uiServer *ui.Server) {
	GeneratedRegister(r)
	CustomizedRegister(r, uiServer)
}
