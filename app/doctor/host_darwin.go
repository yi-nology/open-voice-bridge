//go:build darwin

package doctor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/bluez"
	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/core"
)

// hostChecks reports the macOS host prerequisites. Key remapping, the
// PipeWire virtual mic and the root helper are Linux-only (see
// docs/design/macos-compat.md); what must hold here is that the remote is
// visible to the system and that the paste target is permitted.
func hostChecks(cfg config.Config) []check {
	checks := []check{
		ok("remap helper not used on macOS (key remapping is Linux-only)"),
		ok("virtual mic not used on macOS (ASR consumes the stream directly)"),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	devices, err := bluez.KnownDevices(ctx)
	if err != nil {
		return append(checks, gap(core.DepBLE, "bluetooth device list: "+err.Error(),
			"open System Settings -> Bluetooth and pair the remote"))
	}
	wanted := strings.ToUpper(strings.TrimSpace(cfg.Address))
	visible := false
	for _, item := range devices {
		if !strings.EqualFold(item.Address, wanted) {
			continue
		}
		visible = true
		if item.Connected {
			checks = append(checks, ok(fmt.Sprintf("remote connected (%s)", item.Name)))
		} else {
			checks = append(checks, warn("remote paired but not connected", "bluetoothctl connect "+cfg.Address))
		}
	}
	if !visible {
		checks = append(checks, gap(core.DepRemote, "remote not visible",
			"pair it in System Settings -> Bluetooth"))
	}
	return append(checks, warn("paste needs Accessibility permission",
		"open System Settings -> Privacy & Security -> Accessibility"))
}
