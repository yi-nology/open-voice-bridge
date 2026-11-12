package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/oekit/bootstrap"
	"github.com/oekit/logx"
	"github.com/oekit/pkg/cli"

	"github.com/yi-nology/open-voice-bridge/app/doctor"
	"github.com/yi-nology/open-voice-bridge/app/runtime"
	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/mcp"
	"github.com/yi-nology/open-voice-bridge/internal/remap"
)

var version = "dev"

func main() {
	app := &cli.App{
		Name:    "xiaomi-remote",
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
					return runtime.RunAgent(runtime.AgentOptions{
						ConfigPath: *path,
						Version:    version,
						Logger:     newLogger("xiaomi-remote"),
						Register:   register,
					})
				},
			},
			{
				Name:    "remap",
				Summary: "run the root evdev/uinput key remapper",
				Run: func(_ context.Context, args []string, _, stderr io.Writer) int {
					return runRemap(args, stderr)
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
					return doctor.Run(*path, newLogger("xiaomi-remote"), stdout)
				},
			},
			{
				Name:    "mcp",
				Summary: "serve one AI client over stdio MCP (read-only history/stats)",
				Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
					return runMCP(ctx, args, stdout, stderr)
				},
			},
		},
	}
	os.Exit(app.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func flagSet(name string, stderr io.Writer) (*flag.FlagSet, *string) {
	fs := cli.NewFlagSet(name, stderr)
	path := fs.String("config", config.DefaultPath(), "config file")
	return fs, path
}

func newLogger(service string) logx.Logger {
	logx.UseSlog(service)
	return logx.WithService(service)
}

func runRemap(args []string, stderr io.Writer) int {
	fs := cli.NewFlagSet("remap", stderr)
	mapping := fs.String("mapping", "/etc/xiaomi-remote/mapping.json", "mapping file")
	socket := fs.String("socket", "/run/xiaomi-remote-helper.sock", "Unix socket")
	group := fs.Int("group", 1000, "socket group id")
	agentUID := fs.Int("agent-uid", 0, "uid allowed to run privileged commands besides root (0 = root only)")
	if _, code := cli.ParseFlags(fs, args); code != cli.ExitOK {
		return code
	}
	logger := newLogger("xiaomi-remote-remap")
	if os.Geteuid() != 0 {
		logger.Error("remap requires root for /dev/input and /dev/uinput")
		return 1
	}
	daemon := remap.New(remap.Config{
		MappingPath: *mapping,
		SocketPath:  *socket,
		SocketGroup: *group,
		AgentUID:    *agentUID,
	})
	app, err := bootstrap.New(
		bootstrap.WithServiceName("xiaomi-remote-remap"),
		bootstrap.WithShutdownTimeout(5*time.Second),
	)
	if err != nil {
		logger.Error("bootstrap", "err", err)
		return 1
	}
	err = app.Run(func(app *bootstrap.App) error {
		app.AddResource("remap", func(ctx context.Context) error {
			return daemon.Close(ctx)
		})
		app.GoBackground("remap", func(ctx context.Context) {
			if err := daemon.Run(ctx); err != nil {
				logger.Error("remapper stopped", "err", err)
				app.RequestShutdown()
			}
		})
		app.Ready()
		<-app.Context().Done()
		return nil
	})
	if err != nil {
		logger.Error("remapper stopped", "err", err)
		return 1
	}
	return 0
}

// runMCP serves one AI client over stdio MCP: read-only history and stats
// behind a per-client token ("本地 Agent 访问").
func runMCP(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := cli.NewFlagSet("mcp", stderr)
	token := fs.String("token", os.Getenv("XIAOMI_REMOTE_TOKEN"), "client token (or XIAOMI_REMOTE_TOKEN)")
	base := fs.String("base", "http://127.0.0.1:8787", "admin API base URL")
	if _, code := cli.ParseFlags(fs, args); code != cli.ExitOK {
		return code
	}
	if strings.TrimSpace(*token) == "" {
		fmt.Fprintln(stderr, "mcp: a token is required — create one in the admin UI under 本地 Agent 访问")
		return 1
	}
	server := mcp.NewServer(*base, *token, nil)
	label, err := server.Verify(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "mcp: token rejected:", err)
		return 1
	}
	fmt.Fprintln(stderr, "mcp: serving", label, "(read-only)")
	if err := server.Serve(ctx, os.Stdin, stdout); err != nil {
		fmt.Fprintln(stderr, "mcp:", err)
		return 1
	}
	return 0
}
