package ui

// reply 是 admin UI 的统一 JSON 应答信封(ARC-11 map 棘轮,#16:free-format
// map 只许留在文档点位,新状态一律 typed view)。字段按需填充;omitempty
// 保证与旧的自由格式字面量逐字段等价——前端(web/ui.html)与既有
// 测试断言的响应形状不变。
type reply struct {
	OK       bool              `json:"ok"`
	Error    string            `json:"error,omitempty"`
	Enabled  *bool             `json:"enabled,omitempty"`
	Allow    *bool             `json:"allow,omitempty"`
	Word     string            `json:"word,omitempty"`
	Backend  string            `json:"backend,omitempty"`
	Slot     *int              `json:"slot,omitempty"`
	Text     string            `json:"text,omitempty"`
	Seconds  *int              `json:"seconds,omitempty"`
	Commands map[string]string `json:"commands,omitempty"`
	// History policy (波 3): retention window and address redaction toggle.
	RetentionDays   *int  `json:"retention_days,omitempty"`
	RedactAddresses *bool `json:"redact_addresses,omitempty"`
	RestartRequired *bool `json:"restart_required,omitempty"`
}
