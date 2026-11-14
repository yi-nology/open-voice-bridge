package ui

// reply 是 admin UI 的统一 JSON 应答信封(ARC-11 map 棘轮,#16:free-format
// map 只许留在文档点位,新状态一律 typed view)。字段按需填充;omitempty
// 保证与旧的 map[string]any 字面量逐字段等价——前端(web/ui.html)与既有
// 测试断言的响应形状不变。
type reply struct {
	OK       bool              `json:"ok"`
	Error    string            `json:"error,omitempty"`
	Enabled  *bool             `json:"enabled,omitempty"`
	Word     string            `json:"word,omitempty"`
	Slot     *int              `json:"slot,omitempty"`
	Text     string            `json:"text,omitempty"`
	Seconds  *int              `json:"seconds,omitempty"`
	Commands map[string]string `json:"commands,omitempty"`
}
