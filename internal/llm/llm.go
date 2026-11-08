// Package llm provides optional post-processing of recognized dictation text.
package llm

import (
	"context"
	"os"
	"strings"
	"time"

	oeerrors "github.com/oekit/pkg/errors"
)

type Config struct {
	Enabled        bool   `json:"enabled"`
	Provider       string `json:"provider"`
	BaseURL        string `json:"base_url"`
	Model          string `json:"model"`
	APIKeyFile     string `json:"api_key_file"`
	Mode           string `json:"mode"`
	Prompt         string `json:"prompt"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

type Preset struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	BaseURL     string `json:"base_url"`
	Model       string `json:"model"`
	ModelHint   string `json:"model_hint"`
	KeyHint     string `json:"key_hint"`
	Description string `json:"description"`
}

type Processor interface {
	Name() string
	Process(ctx context.Context, text string) (string, error)
	Ping(ctx context.Context) (time.Duration, error)
	Close(ctx context.Context) error
}

func Presets() []Preset {
	return []Preset{
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

func PresetByID(id string) (Preset, bool) {
	for _, preset := range Presets() {
		if preset.ID == id {
			return preset, true
		}
	}
	return Preset{}, false
}

func DefaultConfig() Config {
	preset, _ := PresetByID("minimax")
	return Config{
		Enabled:        false,
		Provider:       preset.ID,
		BaseURL:        preset.BaseURL,
		Model:          preset.Model,
		APIKeyFile:     "",
		Mode:           "cleanup",
		TimeoutSeconds: 20,
	}
}

func Build(cfg Config) (Processor, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, oeerrors.New("llm base_url is required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, oeerrors.New("llm model is required")
	}
	key := ""
	if path := strings.TrimSpace(cfg.APIKeyFile); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, oeerrors.Errorf("read llm API key: %w", err)
		}
		key = strings.TrimSpace(string(data))
		if key == "" {
			return nil, oeerrors.New("llm API key file is empty")
		}
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	return NewOpenAI(OpenAIConfig{
		BaseURL: cfg.BaseURL,
		APIKey:  key,
		Model:   cfg.Model,
		Mode:    cfg.Mode,
		Prompt:  cfg.Prompt,
		Timeout: timeout,
	})
}

type Diagnostic struct {
	OK     bool
	Detail string
}

func Diagnose(cfg Config) []Diagnostic {
	var out []Diagnostic
	if !cfg.Enabled {
		return []Diagnostic{{OK: true, Detail: "LLM post-processing disabled"}}
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		out = append(out, Diagnostic{OK: false, Detail: "base_url is empty"})
	}
	if strings.TrimSpace(cfg.Model) == "" {
		out = append(out, Diagnostic{OK: false, Detail: "model is empty"})
	}
	if path := strings.TrimSpace(cfg.APIKeyFile); path != "" {
		info, err := os.Stat(path)
		if err != nil {
			out = append(out, Diagnostic{OK: false, Detail: "API key file: " + err.Error()})
		} else if info.Mode().Perm()&0o077 != 0 {
			out = append(out, Diagnostic{OK: true, Detail: "API key readable by group/other; chmod 600 recommended"})
		} else {
			out = append(out, Diagnostic{OK: true, Detail: "API key: " + path})
		}
	} else {
		out = append(out, Diagnostic{OK: true, Detail: "no API key file (local/unauth endpoint)"})
	}
	if len(out) == 0 {
		out = append(out, Diagnostic{OK: true, Detail: "config looks valid"})
	}
	return out
}
