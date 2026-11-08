package asr

import (
	"context"
	oeerrors "github.com/oekit/pkg/errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/atvv"
)

// CommandConfig adapts any local ASR CLI that accepts a WAV path and prints
// text on stdout. It never invokes a shell.
type CommandConfig struct {
	Binary    string
	ModelPath string
	Language  string
	Args      []string
	Timeout   time.Duration
}

type Command struct {
	cfg CommandConfig
}

func NewCommand(cfg CommandConfig) (*Command, error) {
	if cfg.Binary == "" {
		return nil, oeerrors.Errorf("command binary is required")
	}
	if len(cfg.Args) == 0 {
		cfg.Args = []string{"{audio}"}
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 120 * time.Second
	}
	return &Command{cfg: cfg}, nil
}

func (c *Command) Name() string {
	return "command " + filepath.Base(c.cfg.Binary)
}

func (c *Command) Local() bool { return true }

func (c *Command) Close(ctx context.Context) error { return nil }

func (c *Command) Transcribe(ctx context.Context, audio Audio) (Result, error) {
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
	language := c.cfg.Language
	if language == "" {
		language = "auto"
	}
	args := make([]string, 0, len(c.cfg.Args))
	for _, arg := range c.cfg.Args {
		arg = strings.ReplaceAll(arg, "{audio}", wavPath)
		arg = strings.ReplaceAll(arg, "{model}", c.cfg.ModelPath)
		arg = strings.ReplaceAll(arg, "{language}", language)
		args = append(args, arg)
	}

	runCtx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, c.cfg.Binary, args...)
	stdout := &limitedBuffer{limit: 1 << 20}
	stderr := &limitedBuffer{limit: 1 << 14}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return Result{}, oeerrors.Errorf("%s failed: %w (%s)", c.cfg.Binary, err, stderr.String())
	}
	text := strings.TrimSpace(stdout.String())
	if text == "" {
		return Result{}, ErrNoSpeech
	}
	return Result{Text: text, Language: language}, nil
}
