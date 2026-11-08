package asr

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWhisperCPPTranscribe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-whisper.sh")
	body := "#!/bin/sh\n" +
		"out=\"\"\n" +
		"while [ $# -gt 0 ]; do\n" +
		"  case \"$1\" in\n" +
		"    -of) shift; out=\"$1\";;\n" +
		"  esac\n" +
		"  shift\n" +
		"done\n" +
		"printf '本地测试' > \"${out}.txt\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	model := filepath.Join(dir, "model.bin")
	if err := os.WriteFile(model, []byte("fake"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine, err := NewWhisperCPP(WhisperCPPConfig{
		Binary:    script,
		ModelPath: model,
		Language:  "auto",
	})
	if err != nil {
		t.Fatalf("new whisper.cpp: %v", err)
	}
	result, err := engine.Transcribe(context.Background(), Audio{
		Samples:    toneSamples(16000),
		SampleRate: 16000,
	})
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if result.Text != "本地测试" {
		t.Fatalf("text = %q", result.Text)
	}
	if !engine.Local() {
		t.Fatalf("whisper.cpp should be local")
	}
}

func TestCommandTranscribe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-asr.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '命令测试'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	engine, err := NewCommand(CommandConfig{
		Binary: script,
		Args:   []string{"{audio}"},
	})
	if err != nil {
		t.Fatalf("new command: %v", err)
	}
	result, err := engine.Transcribe(context.Background(), Audio{
		Samples:    toneSamples(16000),
		SampleRate: 16000,
	})
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if result.Text != "命令测试" {
		t.Fatalf("text = %q", result.Text)
	}
}
