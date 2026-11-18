package config

// 配置面的 typed 视图（ARC-11 map 棘轮第四次清偿）：admin UI 的配置读取
// 曾以自由格式 map 过手，字段靠键名和类型断言对齐，改名是静默零值。
// 视图与旧载荷的键名逐字段对应（json 标签即旧键名），wire 形状不变。

// ASRConfigView hands the ASR table out as a copy (R6 owned state): callers
// may mutate Providers without touching the running configuration.
type ASRConfigView struct {
	OK        bool                         `json:"ok"`
	Provider  string                       `json:"provider"`
	Fallback  []string                     `json:"fallback"`
	Language  string                       `json:"language"`
	Providers map[string]ASRProviderConfig `json:"providers"`
}

// LLMConfigView is the LLM section plus its preset catalogue.
type LLMConfigView struct {
	OK      bool        `json:"ok"`
	Presets []LLMPreset `json:"presets"`
	Config  LLMConfig   `json:"config"`
}

// TextConfigView is the rewrite dictionary/snippets section.
type TextConfigView struct {
	OK     bool       `json:"ok"`
	Config TextConfig `json:"config"`
}
