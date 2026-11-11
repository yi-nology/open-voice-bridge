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
	"github.com/yi-nology/open-voice-bridge/internal/core"
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
	if err := guardBinary(cfg.Binary); err != nil {
		return nil, err
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
		rate = core.SampleRate
	}
	if len(audio.Samples) < core.MinUtterance(rate) || atvv.Peak(audio.Samples) < 400 {
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
		// stderr comes from a command the user configured themselves, and ASR
		// front-ends do print recognised speech there, so the text must not
		// ride along in an error that gets logged. The size stays in the
		// message; the content is only written at debug level, for whoever is
		// debugging their own wrapper (SEC-02).
		logged := stderr.String()
		if logged != "" {
			log.Debug("ASR command stderr", "binary", c.cfg.Binary, "stderr", logged)
		}
		return Result{}, oeerrors.Errorf("%s failed: %w (stderr %d bytes, see debug log)",
			c.cfg.Binary, err, len(logged))
	}
	text := strings.TrimSpace(stdout.String())
	if text == "" {
		return Result{}, ErrNoSpeech
	}
	return Result{Text: text, Language: language}, nil
}
