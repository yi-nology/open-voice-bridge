package config

// The section shapes live here so config stays a leaf (R1). A subsystem owns
// the behaviour and the vocabulary; the document that carries its settings is
// config's, and internal/appconfig is where the two meet.

// DefaultASRModel is the MiMo model every default points at, so the provider
// client and the generated config cannot drift apart.
const DefaultASRModel = "mimo-v2.5-asr"

type ASRProviderConfig struct {
	Type           string   `json:"type"`
	APIKeyFile     string   `json:"api_key_file"`
	BaseURL        string   `json:"base_url"`
	Model          string   `json:"model"`
	Language       string   `json:"language"`
	Binary         string   `json:"binary"`
	ModelPath      string   `json:"model_path"`
	Threads        int      `json:"threads"`
	Args           []string `json:"args"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}

type ASRConfig struct {
	Provider  string                       `json:"provider"`
	Fallback  []string                     `json:"fallback"`
	Language  string                       `json:"language"`
	Providers map[string]ASRProviderConfig `json:"providers"`
}

func DefaultASR() ASRConfig {
	return ASRConfig{
		Provider: "mimo",
		Language: "auto",
		Providers: map[string]ASRProviderConfig{
			"mimo": {
				Type:     "mimo",
				Model:    DefaultASRModel,
				Language: "auto",
			},
		},
	}
}

type LLMConfig struct {
	Enabled        bool   `json:"enabled"`
	Provider       string `json:"provider"`
	BaseURL        string `json:"base_url"`
	Model          string `json:"model"`
	APIKeyFile     string `json:"api_key_file"`
	Mode           string `json:"mode"`
	Prompt         string `json:"prompt"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

type LLMPreset struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	BaseURL     string `json:"base_url"`
	Model       string `json:"model"`
	ModelHint   string `json:"model_hint"`
	KeyHint     string `json:"key_hint"`
	Description string `json:"description"`
}

func LLMPresets() []LLMPreset {
	return []LLMPreset{
		{
			ID:          "minimax",
			Name:        "MiniMax",
			BaseURL:     "https://api.minimaxi.com/v1",
			Model:       "MiniMax-M3.1-Flash",
			ModelHint:   "MiniMax-M3.1-Flash / MiniMax-M3",
			KeyHint:     "MiniMax 开放平台 API Key",
			Description: "OpenAI 兼容 Chat Completions",
		},
		{
			ID:          "volcengine",
			Name:        "火山方舟（豆包）",
			BaseURL:     "https://ark.cn-beijing.volces.com/api/v3",
			Model:       "",
			ModelHint:   "推理接入点 ID（ep-...）或火山方舟模型 ID",
			KeyHint:     "火山方舟 API Key",
			Description: "OpenAI 兼容 Chat Completions",
		},
		{
			ID:          "openai",
			Name:        "OpenAI",
			BaseURL:     "https://api.openai.com/v1",
			Model:       "gpt-4o-mini",
			ModelHint:   "gpt-4o-mini / gpt-4.1-mini",
			KeyHint:     "OpenAI API Key",
			Description: "OpenAI Chat Completions",
		},
		{
			ID:          "deepseek",
			Name:        "DeepSeek",
			BaseURL:     "https://api.deepseek.com/v1",
			Model:       "deepseek-chat",
			ModelHint:   "deepseek-chat / deepseek-reasoner",
			KeyHint:     "DeepSeek API Key",
			Description: "OpenAI 兼容 Chat Completions",
		},
		{
			ID:          "custom",
			Name:        "自定义 OpenAI 兼容",
			BaseURL:     "",
			Model:       "",
			ModelHint:   "模型名",
			KeyHint:     "API Key 文件",
			Description: "任意 OpenAI 兼容 /chat/completions 服务",
		},
	}
}

func LLMPresetByID(id string) (LLMPreset, bool) {
	for _, preset := range LLMPresets() {
		if preset.ID == id {
			return preset, true
		}
	}
	return LLMPreset{}, false
}

func DefaultLLM() LLMConfig {
	preset, _ := LLMPresetByID("minimax")
	return LLMConfig{
		Enabled:        false,
		Provider:       preset.ID,
		BaseURL:        preset.BaseURL,
		Model:          preset.Model,
		APIKeyFile:     "",
		Mode:           "cleanup",
		TimeoutSeconds: 20,
	}
}

type Replacement struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type Snippet struct {
	Trigger string `json:"trigger"`
	Text    string `json:"text"`
}

type TextConfig struct {
	Dictionary []Replacement `json:"dictionary"`
	Snippets   []Snippet     `json:"snippets"`
}

// DefaultKeymap is the coding profile's starting point. The action vocabulary
// it draws from stays in internal/remap, which validates against it.
var DefaultKeymap = map[string]string{
	"power":        "esc",
	"f5":           "f24",
	"up":           "native",
	"down":         "native",
	"left":         "native",
	"right":        "native",
	"enter":        "native",
	"back":         "esc",
	"home":         "macro(leftalt+tab)",
	"compose":      "native",
	"grave":        "noop",
	"volumeup":     "native",
	"volumedown":   "native",
	"ok":           "enter",
	"homepage":     "macro(leftalt+tab)",
	"menu":         "compose",
	"playpause":    "playpause",
	"nextsong":     "nextsong",
	"previoussong": "previoussong",
	"mute":         "mute",
}
