package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/oekit/logx"
	"github.com/oekit/pkg/cli"

	"github.com/yi-nology/open-voice-bridge/app/doctor"
	"github.com/yi-nology/open-voice-bridge/app/runtime"
	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/diagbox"
	"github.com/yi-nology/open-voice-bridge/internal/mcp"
)

var version = "dev"

func main() {
	app := &cli.App{
		Name:    "xiaoyao",
		Version: version,
		Commands: []cli.Command{
			{
				Name:    "agent",
				Summary: "run the BLE/ATVV, ASR, and local UI agent",
				Run: func(_ context.Context, args []string, _, stderr io.Writer) int {
					fs, path := flagSet("agent", stderr)
					if _, code := cli.ParseFlags(fs, args); code != cli.ExitOK {
						return code
					}
					applyLogLevel(fs)
					return runtime.RunAgent(runtime.AgentOptions{
						ConfigPath: *path,
						Version:    version,
						Logger:     newLogger("xiaoyao"),
						Register:   register,
					})
				},
			},
			{
				Name:    "remap",
				Summary: "run the root evdev/uinput key remapper",
				Run: func(_ context.Context, args []string, _, stderr io.Writer) int {
					fs := cli.NewFlagSet("remap", stderr)
					mapping := fs.String("mapping", "/etc/xiaoyao/mapping.json", "mapping file")
					socket := fs.String("socket", "/run/xiaoyao-helper.sock", "Unix socket")
					group := fs.Int("group", 1000, "socket group id")
					agentUID := fs.Int("agent-uid", 0, "uid allowed to run privileged commands besides root (0 = root only)")
					if _, code := cli.ParseFlags(fs, args); code != cli.ExitOK {
						return code
					}
					return runtime.RunRemap(runtime.RemapOptions{
						MappingPath: *mapping,
						SocketPath:  *socket,
						SocketGroup: *group,
						AgentUID:    *agentUID,
						Logger:      newLogger("xiaoyao-remap"),
					})
				},
			},
			{
				Name:    "doctor",
				Summary: "check config, services, devices, and ASR providers",
				Run: func(_ context.Context, args []string, stdout, stderr io.Writer) int {
					fs, path := flagSet("doctor", stderr)
					if _, code := cli.ParseFlags(fs, args); code != cli.ExitOK {
						return code
					}
					applyLogLevel(fs)
					return doctor.Run(*path, newLogger("xiaoyao"), stdout)
				},
			},
			{
				Name:    "mcp",
				Summary: "serve one AI client over stdio MCP (read-only history/stats)",
				Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
					return runMCP(ctx, args, stdout, stderr)
				},
			},
			{
				Name:    "reset",
				Summary: "clear local state (history, usage, tokens) after confirmation",
				Run: func(_ context.Context, args []string, stdout, stderr io.Writer) int {
					fs := cli.NewFlagSet("reset", stderr)
					path := fs.String("config", config.DefaultPath(), "config file")
					yes := fs.Bool("yes", false, "skip the confirmation prompt")
					if _, code := cli.ParseFlags(fs, args); code != cli.ExitOK {
						return code
					}
					return runReset(*path, *yes, stdout, stderr)
				},
			},
			{
				Name:    "diag",
				Summary: "decrypt an exported diagnostics pack (passphrase prompted on stderr)",
				Run: func(_ context.Context, args []string, stdout, stderr io.Writer) int {
					fs := cli.NewFlagSet("diag", stderr)
					out := fs.String("o", "", "write plaintext to this file instead of stdout")
					rest, code := cli.ParseFlags(fs, args)
					if code != cli.ExitOK {
						return code
					}
					if len(rest) != 1 {
						_, _ = fmt.Fprintln(stderr, "usage: xiaoyao diag <pack.ovbdiag> [-o out.json]")
						return 2
					}
					return runDiagDecrypt(rest[0], *out, stdout, stderr)
				},
			},
		},
	}
	os.Exit(app.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

// runReset deletes the local state files beside the config — history, usage
// stats and every token store — after an explicit confirmation. The config
// file itself is kept: losing settings is not what "reset" means (OPS-12).
func runReset(path string, assumeYes bool, stdout, stderr io.Writer) int {
	dir := filepath.Dir(path)
	if !assumeYes {
		_, _ = fmt.Fprint(stdout, "This deletes history, usage stats and all tokens in ", dir, ".\nType yes to confirm: ")
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil || strings.TrimSpace(line) != "yes" {
			_, _ = fmt.Fprintln(stdout, "aborted; nothing was deleted")
			return 1
		}
	}
	targets := []string{"history.jsonl", "usage-stats.json", "phone_tokens.json", "mcp-tokens.json"}
	removed := 0
	for _, name := range targets {
		target := filepath.Join(dir, name)
		switch err := os.Remove(target); {
		case err == nil:
			_, _ = fmt.Fprintln(stdout, "removed", target)
			removed++
		case os.IsNotExist(err):
		default:
			_, _ = fmt.Fprintln(stderr, "cannot remove", target, err)
			return 1
		}
	}
	_, _ = fmt.Fprintf(stdout, "reset complete: %d file(s) removed; config kept at %s\n", removed, path)
	return 0
}

// runDiagDecrypt opens an encrypted diagnostics pack (R10) and writes the
// plaintext out. The passphrase is prompted on stderr so stdout stays pipe
// clean: xiaoyao diag pack.ovbdiag | jq .
func runDiagDecrypt(packPath, outPath string, stdout, stderr io.Writer) int {
	pack, err := os.ReadFile(packPath)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "read pack:", err)
		return 1
	}
	_, _ = fmt.Fprint(stderr, "passphrase: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		_, _ = fmt.Fprintln(stderr, "read passphrase:", err)
		return 1
	}
	plain, err := diagbox.Open(pack, strings.TrimRight(line, "\r\n"))
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "decrypt:", err)
		return 1
	}
	if outPath == "" {
		_, _ = stdout.Write(plain)
		if len(plain) == 0 || plain[len(plain)-1] != '\n' {
			_, _ = fmt.Fprintln(stdout)
		}
		return 0
	}
	if err := os.WriteFile(outPath, plain, 0o600); err != nil {
		_, _ = fmt.Fprintln(stderr, "write:", err)
		return 1
	}
	_, _ = fmt.Fprintln(stdout, "wrote", outPath)
	return 0
}

func flagSet(name string, stderr io.Writer) (*flag.FlagSet, *string) {
	fs := cli.NewFlagSet(name, stderr)
	path := fs.String("config", config.DefaultPath(), "config file")
	fs.String("log-level", "info", "log level: DEBUG | INFO | WARN | ERROR (OPS-12)")
	return fs, path
}

// applyLogLevel hands the flag to the logging backend, which reads
// LOG_LEVEL for every logger (OPS-12).
func applyLogLevel(fs *flag.FlagSet) {
	flag := fs.Lookup("log-level")
	if flag == nil {
		return
	}
	level := strings.ToUpper(strings.TrimSpace(flag.Value.String()))
	switch level {
	case "DEBUG", "INFO", "WARN", "WARNING", "ERROR":
		_ = os.Setenv("LOG_LEVEL", level)
	}
}

func newLogger(service string) logx.Logger {
	logx.UseSlog(service)
	return logx.WithService(service)
}

// runMCP serves one AI client over stdio MCP: read-only history and stats
// behind a per-client token ("本地 Agent 访问").
func runMCP(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := cli.NewFlagSet("mcp", stderr)
	token := fs.String("token", os.Getenv("XIAOYAO_TOKEN"), "client token (or XIAOYAO_TOKEN)")
	base := fs.String("base", "http://127.0.0.1:8787", "admin API base URL")
	if _, code := cli.ParseFlags(fs, args); code != cli.ExitOK {
		return code
	}
	if strings.TrimSpace(*token) == "" {
		_, _ = fmt.Fprintln(stderr, "mcp: a token is required — create one in the admin UI under 本地 Agent 访问")
		return 1
	}
	server := mcp.NewServer(*base, *token, nil)
	label, err := server.Verify(ctx)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "mcp: token rejected:", err)
		return 1
	}
	_, _ = fmt.Fprintln(stderr, "mcp: serving", label, "(read-only)")
	if err := server.Serve(ctx, os.Stdin, stdout); err != nil {
		_, _ = fmt.Fprintln(stderr, "mcp:", err)
		return 1
	}
	return 0
}
