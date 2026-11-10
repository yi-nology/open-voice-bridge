package asr

import (
	"context"
	oeerrors "github.com/oekit/pkg/errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/atvv"
)

type WhisperCPPConfig struct {
	Binary    string
	ModelPath string
	Language  string
	Threads   int
	Timeout   time.Duration
	ExtraArgs []string
}

type WhisperCPP struct {
	cfg WhisperCPPConfig
}

func NewWhisperCPP(cfg WhisperCPPConfig) (*WhisperCPP, error) {
	if cfg.Binary == "" {
		cfg.Binary = "whisper-cli"
	}
	if err := guardBinary(cfg.Binary); err != nil {
		return nil, err
	}
	if cfg.ModelPath == "" {
		return nil, oeerrors.Errorf("whisper.cpp model_path is required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 120 * time.Second
	}
	return &WhisperCPP{cfg: cfg}, nil
}

func (w *WhisperCPP) Name() string {
	return "whisper.cpp " + filepath.Base(w.cfg.ModelPath)
}

func (w *WhisperCPP) Local() bool { return true }

func (w *WhisperCPP) Close(ctx context.Context) error { return nil }

func (w *WhisperCPP) Transcribe(ctx context.Context, audio Audio) (Result, error) {
	rate := audio.SampleRate
	if rate == 0 {
		rate = atvv.SampleRate
	}
	if len(audio.Samples) < rate/4 || atvv.Peak(audio.Samples) < 400 {
		return Result{}, ErrNoSpeech
	}
	dir, err := os.MkdirTemp("", "xiaomi-asr-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(dir)
	wavPath := filepath.Join(dir, "audio.wav")
	if err := os.WriteFile(wavPath, WAV(audio.Samples, rate), 0o600); err != nil {
		return Result{}, err
	}
	outBase := filepath.Join(dir, "out")
	language := w.cfg.Language
	if language == "" {
		language = "auto"
	}
	args := []string{
		"-m", w.cfg.ModelPath,
		"-f", wavPath,
		"-l", language,
		"-otxt",
		"-of", outBase,
		"-np",
		"-nt",
	}
	if w.cfg.Threads > 0 {
		args = append(args, "-t", strconv.Itoa(w.cfg.Threads))
	}
	args = append(args, w.cfg.ExtraArgs...)

	runCtx, cancel := context.WithTimeout(ctx, w.cfg.Timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, w.cfg.Binary, args...)
	var stderr limitedBuffer
	cmd.Stderr = &stderr
	cmd.Stdout = io.Discard
	if err := cmd.Run(); err != nil {
		return Result{}, oeerrors.Errorf("whisper.cpp failed: %w (%s)", err, stderr.String())
	}
	data, err := os.ReadFile(outBase + ".txt")
	if err != nil {
		return Result{}, oeerrors.Errorf("whisper.cpp output missing: %w", err)
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return Result{}, ErrNoSpeech
	}
	return Result{Text: text, Language: language}, nil
}
