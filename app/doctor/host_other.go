//go:build !linux && !darwin

package doctor

import (
	"fmt"
	"io"

	"github.com/yi-nology/open-voice-bridge/internal/config"
)

func hostChecks(stdout io.Writer, cfg config.Config) int {
	fmt.Fprintln(stdout, "WARN: host checks not implemented on this platform")
	return 0
}
