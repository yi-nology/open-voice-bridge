//go:build !linux && !darwin

package main

import (
	"fmt"
	"io"

	"github.com/yi-nology/open-voice-bridge/internal/config"
)

func doctorHostChecks(stdout io.Writer, cfg config.Config) int {
	fmt.Fprintln(stdout, "WARN: host checks not implemented on this platform")
	return 0
}
