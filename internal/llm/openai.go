package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	oeerrors "github.com/oekit/pkg/errors"

	"github.com/yi-nology/open-voice-bridge/internal/redact"
)

type OpenAIConfig struct {
	BaseURL string
	APIKey  string
	Model   string
	Mode    string
	Prompt  string
	Timeout time.Duration
}

type OpenAI struct {
	cfg    OpenAIConfig
	client *http.Client
}

func NewOpenAI(cfg OpenAIConfig) (*OpenAI, error) {
	parsed, err := url.Parse(cfg.BaseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, oeerrors.Errorf("invalid llm base_url %q", cfg.BaseURL)
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.Timeout <= 0 {
		cfg.Timeout = 20 * time.Second
	}
	return &OpenAI{
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.Timeout},
	}, nil
}

func (o *OpenAI) Name() string { return "llm " + o.cfg.Model }

func (o *OpenAI) Close(ctx context.Context) error { return nil }

func (o *OpenAI) Ping(ctx context.Context) (time.Duration, error) {
	start := time.Now()
	_, err := o.Process(ctx, "只回复 OK")
	return time.Since(start), err
}

// ProcessMode rewrites with an explicit mode, overriding the configured one
// for this call — the app-aware rewrite path (S5). Empty mode = default.
func (o *OpenAI) ProcessMode(ctx context.Context, text, mode string) (string, error) {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		mode = o.cfg.Mode
	}
	saved := o.cfg.Mode
	o.cfg.Mode = mode
	defer func() { o.cfg.Mode = saved }()
	return o.Process(ctx, text)
}

func (o *OpenAI) Process(ctx context.Context, text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}
	payload := map[string]any{
		"model":       o.cfg.Model,
		"temperature": 0.2,
		"max_tokens":  1024,
		"messages": []any{
			map[string]any{"role": "system", "content": systemPrompt(o.cfg.Mode, o.cfg.Prompt)},
			map[string]any{"role": "user", "content": text},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if o.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.cfg.APIKey)
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return "", oeerrors.Errorf("llm request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		// The body is provider prose and a rejected request often quotes the
		// text it rejected, which here is a transcript. Errors get logged, so
		// only the status and the machine-readable codes are kept (SEC-02).
		return "", oeerrors.Errorf("llm HTTP %d%s", resp.StatusCode, redact.ProviderSuffix(data))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", oeerrors.Errorf("parse llm response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", oeerrors.New("llm returned no choices")
	}
	content := decodeContent(parsed.Choices[0].Message.Content)
	content = strings.TrimSpace(content)
	if content == "" {
		return "", oeerrors.New("llm returned empty text")
	}
	return content, nil
}

func systemPrompt(mode, custom string) string {
	if strings.TrimSpace(custom) != "" {
		return custom
	}
	base := "你是语音转写文本的后处理器。只输出处理后的文本，不要解释、不要加引号或前缀。"
	switch mode {
	case "format":
		return base + " 把内容整理成清晰的段落，保留原意和语言。"
	case "translate":
		return base + " 把内容翻译成英文，只输出译文。"
	default:
		return base + " 去掉“嗯、呃、那个”等口语填充词，补全标点，修正明显的同音错字，保持原意和原语言。"
	}
}

func decodeContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var b strings.Builder
		for _, part := range parts {
			b.WriteString(part.Text)
		}
		return b.String()
	}
	return ""
}
