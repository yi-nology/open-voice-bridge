package runtime

import (
	"context"
	"os"
	"time"

	"github.com/oekit/bootstrap"
	"github.com/oekit/logx"

	"github.com/yi-nology/open-voice-bridge/internal/remap"
)

// RemapOptions is what the remap command needs from the CLI layer: the parsed
// flags and a logger.
type RemapOptions struct {
	MappingPath string
	SocketPath  string
	SocketGroup int
	AgentUID    int
	Logger      logx.Logger
}

// RunRemap runs the root evdev/uinput key remapper and blocks until the
// process is asked to stop. The daemon failing is fatal here, unlike the
// agent's degraded subsystems: remapping is this command's whole job.
func RunRemap(opts RemapOptions) int {
	logger := opts.Logger
	if os.Geteuid() != 0 {
		logger.Error("remap requires root for /dev/input and /dev/uinput")
		return 1
	}
	daemon := remap.New(remap.Config{
		MappingPath: opts.MappingPath,
		SocketPath:  opts.SocketPath,
		SocketGroup: opts.SocketGroup,
		AgentUID:    opts.AgentUID,
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
