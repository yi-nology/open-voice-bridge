// Package miccap reads 16 kHz mono PCM from the desktop's default
// microphone so the agent can hear a wake word without any key press (S7).
// It shells out to whatever capture tool the platform ships or the user
// installed — there is no audio dependency in the Go build.
package miccap

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os/exec"
	"runtime"
	"strconv"
	"sync"

	"github.com/yi-nology/open-voice-bridge/internal/core"
)

// ErrNoCaptureTool means neither pw-cat/arecord (Linux) nor sox/ffmpeg
// (macOS) is on PATH; the doctor names the fix.
var ErrNoCaptureTool = errors.New("no microphone capture tool found (install sox or ffmpeg, or pw-cat/arecord on Linux)")

// FrameSource yields captured PCM frames; the wake loop consumes it and
// tests substitute their own.
type FrameSource interface {
	Frames() <-chan []int16
	Err() error
	Close() error
}

// Capturer runs one capture tool and demuxes its raw little-endian s16
// stdout into ~100 ms frames.
type Capturer struct {
	cmd     *exec.Cmd
	frames  chan []int16
	stop    chan struct{}
	stopOne sync.Once
	mu      sync.Mutex
	err     error
}

// Start launches the platform capture tool. Device is an optional device
// name understood by the tool ("" = default input).
func Start(ctx context.Context, device string) (*Capturer, error) {
	cmd, err := captureCommand(ctx, device)
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &Capturer{cmd: cmd, frames: make(chan []int16, 8), stop: make(chan struct{})}
	go c.demux(stdout)
	return c, nil
}

// Frames yields captured PCM frames until the source ends.
func (c *Capturer) Frames() <-chan []int16 { return c.frames }

// Err reports why the stream ended, if it ended badly.
func (c *Capturer) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Close stops the capture tool and releases the device.
func (c *Capturer) Close() error {
	c.stopOne.Do(func() { close(c.stop) })
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	return c.cmd.Wait()
}

func (c *Capturer) demux(stdout io.Reader) {
	defer close(c.frames)
	// 100 ms of 16 kHz mono s16 per frame.
	buf := make([]byte, core.SampleRate/10*2)
	for {
		if _, err := io.ReadFull(stdout, buf); err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
				c.mu.Lock()
				c.err = err
				c.mu.Unlock()
			}
			return
		}
		frame := make([]int16, len(buf)/2)
		for index := range frame {
			frame[index] = int16(binary.LittleEndian.Uint16(buf[index*2:]))
		}
		select {
		case c.frames <- frame:
		case <-c.stop:
			return
		}
	}
}

// captureCommand picks the platform's capture tool.
func captureCommand(ctx context.Context, device string) (*exec.Cmd, error) {
	// The tools want the rate as a flag value; core owns the number (R5).
	rate := strconv.Itoa(core.SampleRate)
	switch runtime.GOOS {
	case "linux":
		if path, err := exec.LookPath("pw-cat"); err == nil {
			args := []string{"--record", "--format", "s16", "--rate", rate, "--channels", "1"}
			if device != "" {
				args = append(args, "--target", device)
			}
			return exec.CommandContext(ctx, path, append(args, "-")...), nil
		}
		if path, err := exec.LookPath("arecord"); err == nil {
			args := []string{"-t", "raw", "-f", "S16_LE", "-r", rate, "-c", "1", "-D", "default"}
			if device != "" {
				args = []string{"-t", "raw", "-f", "S16_LE", "-r", rate, "-c", "1", "-D", device}
			}
			return exec.CommandContext(ctx, path, args...), nil
		}
		return nil, ErrNoCaptureTool
	case "darwin":
		if path, err := exec.LookPath("sox"); err == nil {
			args := []string{"-q", "-d", "-t", "raw", "-b", "16", "-e", "signed-integer", "-r", rate, "-c", "1", "-"}
			return exec.CommandContext(ctx, path, args...), nil
		}
		if path, err := exec.LookPath("ffmpeg"); err == nil {
			input := ":default"
			if device != "" {
				input = device
			}
			return exec.CommandContext(ctx, path,
				"-nostdin", "-loglevel", "quiet", "-f", "avfoundation", "-i", input,
				"-ar", rate, "-ac", "1", "-f", "s16le", "pipe:1"), nil
		}
		return nil, ErrNoCaptureTool
	default:
		return nil, ErrNoCaptureTool
	}
}
