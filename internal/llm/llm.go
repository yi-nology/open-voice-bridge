// Package llm provides optional post-processing of recognized dictation text.
package llm

import (
	"context"
	"os"
	"strings"
	"time"

	oeerrors "github.com/oekit/pkg/errors"

	"github.com/yi-nology/open-voice-bridge/internal/config"
)

type Processor interface {
	Name() string
	Process(ctx context.Context, text string) (string, error)
	Ping(ctx context.Context) (time.Duration, error)
	Close(ctx context.Context) error
}

func Build(cfg config.LLMConfig) (Processor, error) {
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

func Diagnose(cfg config.LLMConfig) []Diagnostic {
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
