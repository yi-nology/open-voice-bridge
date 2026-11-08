package asr

import (
	"bytes"
	"context"
	"encoding/json"
	oeerrors "github.com/oekit/pkg/errors"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/atvv"
)

// OpenAIConfig works with OpenAI, Groq, and local servers that implement
// POST /v1/audio/transcriptions (whisper.cpp server, faster-whisper-server,
// LocalAI, etc.).
type OpenAIConfig struct {
	BaseURL  string
	APIKey   string
	Model    string
	Language string
	Timeout  time.Duration
}

type OpenAI struct {
	cfg    OpenAIConfig
	client *http.Client
}

func NewOpenAI(cfg OpenAIConfig) (*OpenAI, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	parsed, err := url.Parse(cfg.BaseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, oeerrors.Errorf("invalid OpenAI-compatible base_url %q", cfg.BaseURL)
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.Model == "" {
		cfg.Model = "whisper-1"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	return &OpenAI{
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.Timeout},
	}, nil
}

func (o *OpenAI) Name() string { return "OpenAI-compatible " + o.cfg.Model }

func (o *OpenAI) Local() bool { return IsLocalURL(o.cfg.BaseURL) }

func (o *OpenAI) Close(ctx context.Context) error { return nil }

func (o *OpenAI) Transcribe(ctx context.Context, audio Audio) (Result, error) {
	rate := audio.SampleRate
	if rate == 0 {
		rate = atvv.SampleRate
	}
	if len(audio.Samples) < rate/4 || atvv.Peak(audio.Samples) < 400 {
		return Result{}, ErrNoSpeech
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		return Result{}, err
	}
	if _, err := part.Write(WAV(audio.Samples, rate)); err != nil {
		return Result{}, err
	}
	if err := writer.WriteField("model", o.cfg.Model); err != nil {
		return Result{}, err
	}
	if o.cfg.Language != "" && o.cfg.Language != "auto" {
		if err := writer.WriteField("language", o.cfg.Language); err != nil {
			return Result{}, err
		}
	}
	if err := writer.WriteField("response_format", "json"); err != nil {
		return Result{}, err
	}
	if err := writer.Close(); err != nil {
		return Result{}, err
	}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, o.cfg.BaseURL+"/audio/transcriptions", &body,
	)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if o.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.cfg.APIKey)
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return Result{}, oeerrors.Errorf("transcription request: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return Result{}, oeerrors.Errorf("transcription HTTP %d: %s", resp.StatusCode, truncate(string(data), 300))
	}
	var parsed struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return Result{}, oeerrors.Errorf("parse transcription: %w", err)
	}
	text := strings.TrimSpace(parsed.Text)
	if text == "" {
		return Result{}, ErrNoSpeech
	}
	return Result{Text: text, Language: o.cfg.Language}, nil
}

// IsLocalURL reports whether the endpoint is loopback or a private LAN host.
func IsLocalURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}
