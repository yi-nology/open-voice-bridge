//go:build darwin

package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/bluez"
	"github.com/yi-nology/open-voice-bridge/internal/config"
)

// doctorHostChecks reports the macOS host prerequisites. Key remapping, the
// PipeWire virtual mic and the root helper are Linux-only (see
// docs/design/macos-compat.md); what must hold here is that the remote is
// visible to the system and that the paste target is permitted.
func doctorHostChecks(stdout io.Writer, cfg config.Config) int {
	failures := 0
	fmt.Fprintln(stdout, "ok: remap helper not used on macOS (key remapping is Linux-only)")
	fmt.Fprintln(stdout, "ok: virtual mic not used on macOS (ASR consumes the stream directly)")

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	devices, err := bluez.KnownDevices(ctx)
	if err != nil {
		fmt.Fprintln(stdout, "FAIL: bluetooth device list:", err)
		return failures + 1
	}
	wanted := strings.ToUpper(strings.TrimSpace(cfg.Address))
	visible := false
	for _, item := range devices {
		if strings.EqualFold(item.Address, wanted) {
			visible = true
			if item.Connected {
				fmt.Fprintf(stdout, "ok: remote connected (%s)\n", item.Name)
			} else {
				fmt.Fprintln(stdout, "WARN: remote paired but not connected")
			}
		}
	}
	if !visible {
		fmt.Fprintln(stdout, "FAIL: remote not visible; pair it in System Settings -> Bluetooth")
		failures++
	}
	fmt.Fprintln(stdout, "WARN: paste needs Accessibility permission (System Settings -> Privacy & Security)")
	return failures
}
