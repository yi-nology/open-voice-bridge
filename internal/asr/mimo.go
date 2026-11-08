// Package asr implements the Xiaomi MiMo-V2.5-ASR API client.
package asr

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	oeerrors "github.com/oekit/pkg/errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/atvv"
)

const (
	DefaultModel   = "mimo-v2.5-asr"
	DefaultBaseURL = "https://api.xiaomimimo.com/v1"
	TokenPlanCN    = "https://token-plan-cn.xiaomimimo.com/v1"
	TokenPlanSGP   = "https://token-plan-sgp.xiaomimimo.com/v1"
	TokenPlanAMS   = "https://token-plan-ams.xiaomimimo.com/v1"
)

type Mimo struct {
	APIKey   string
	Model    string
	Language string
	BaseURL  string
	Client   *http.Client
}

// NewMimo reads the API key from keyFile and selects the matching regional
// endpoint when baseURL is empty.
func NewMimo(keyFile, model, language, baseURL string) (*Mimo, error) {
	key, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, oeerrors.Errorf("read MiMo API key: %w", err)
	}
	apiKey := strings.TrimSpace(string(key))
	if apiKey == "" {
		return nil, oeerrors.Errorf("MiMo API key file is empty")
	}
	if model == "" {
		model = DefaultModel
	}
	if language == "" {
		language = "auto"
	}
	if baseURL == "" {
		switch {
		case strings.HasPrefix(apiKey, "tp-"), strings.HasPrefix(apiKey, "ttp-"):
			baseURL = TokenPlanCN
		case strings.HasPrefix(apiKey, "sk-"):
			baseURL = DefaultBaseURL
		default:
			baseURL = DefaultBaseURL
		}
	}
	return &Mimo{
		APIKey:   apiKey,
		Model:    model,
		Language: language,
		BaseURL:  strings.TrimRight(baseURL, "/"),
		Client: &http.Client{
			Timeout: 35 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        4,
				IdleConnTimeout:     60 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
		},
	}, nil
}

func (m *Mimo) Name() string { return "MiMo " + m.Model }

func (m *Mimo) Local() bool { return false }

func (m *Mimo) Close(ctx context.Context) error { return nil }

// Transcribe sends one 16 kHz mono utterance and returns the recognized text.
func (m *Mimo) Transcribe(ctx context.Context, audio Audio) (Result, error) {
	rate := audio.SampleRate
	if rate == 0 {
		rate = atvv.SampleRate
	}
	if rate != atvv.SampleRate {
		return Result{}, oeerrors.Errorf("MiMo requires 16 kHz audio, got %d", rate)
	}
	if len(audio.Samples) < rate/4 || atvv.Peak(audio.Samples) < 400 {
		return Result{}, ErrNoSpeech
	}
	encoded := base64.StdEncoding.EncodeToString(WAV(audio.Samples, rate))
	payload := map[string]any{
		"model": m.Model,
		"messages": []any{map[string]any{
			"role": "user",
			"content": []any{map[string]any{
				"type": "input_audio",
				"input_audio": map[string]any{
					"data": "data:audio/wav;base64," + encoded,
				},
			}},
		}},
		"asr_options": map[string]any{"language": m.Language},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, m.BaseURL+"/chat/completions", bytes.NewReader(body),
	)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("api-key", m.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.Client.Do(req)
	if err != nil {
		return Result{}, oeerrors.Errorf("MiMo request: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return Result{}, oeerrors.Errorf("MiMo HTTP %d: %s", resp.StatusCode, truncate(string(data), 300))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return Result{}, oeerrors.Errorf("parse MiMo response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return Result{}, ErrNoSpeech
	}
	text := decodeContent(parsed.Choices[0].Message.Content)
	if text == "" {
		return Result{}, ErrNoSpeech
	}
	return Result{Text: text, Language: m.Language}, nil
}

func decodeContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var builder strings.Builder
		for _, part := range parts {
			builder.WriteString(part.Text)
		}
		return strings.TrimSpace(builder.String())
	}
	return ""
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit]
}
