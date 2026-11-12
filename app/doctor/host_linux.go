//go:build linux

package doctor

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/helper"
	"github.com/yi-nology/open-voice-bridge/internal/remap"
)

// hostChecks reports the Linux host prerequisites: root remap helper,
// PipeWire/X11 tooling, systemd units, uinput and the evdev remote.
func hostChecks(stdout io.Writer, cfg config.Config) int {
	failures := 0
	doctorCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if helperStatus, err := helper.New(cfg.HelperSocket).Call(doctorCtx, map[string]string{"cmd": "status"}); err != nil {
		fmt.Fprintln(stdout, "FAIL: remap helper:", err)
		failures++
	} else {
		fmt.Fprintf(stdout, "ok: remap helper (active=%v, remote_present=%v)\n",
			helperStatus["keyd_active"], helperStatus["remote_present"])
	}
	for _, binary := range []string{"pw-cat", "pw-loopback", "xsel", "bluetoothctl"} {
		if _, err := exec.LookPath(binary); err != nil {
			fmt.Fprintln(stdout, "FAIL: missing", binary)
			failures++
		} else {
			fmt.Fprintln(stdout, "ok:", binary)
		}
	}
	for _, unit := range []struct {
		label string
		args  []string
	}{
		{"agent active", []string{"--user", "is-active", "xiaomi-remote-go"}},
		{"agent autostart", []string{"--user", "is-enabled", "xiaomi-remote-go"}},
		{"remap active", []string{"is-active", "xiaomi-remote-remap"}},
		{"remap autostart", []string{"is-enabled", "xiaomi-remote-remap"}},
	} {
		out, err := exec.Command("systemctl", unit.args...).Output()
		state := strings.TrimSpace(string(out))
		if err != nil {
			fmt.Fprintf(stdout, "WARN: %s: %s\n", unit.label, state)
			continue
		}
		fmt.Fprintf(stdout, "ok: %s: %s\n", unit.label, state)
	}
	if current, err := user.Current(); err == nil {
		out, err := exec.Command("loginctl", "show-user", current.Username, "-p", "Linger").Output()
		if err == nil && strings.Contains(string(out), "Linger=yes") {
			fmt.Fprintln(stdout, "ok: linger enabled (starts before login)")
		} else {
			fmt.Fprintln(stdout, "WARN: linger disabled; agent will not start until you log in")
		}
	}
	if info, err := os.Stat("/dev/uinput"); err != nil {
		fmt.Fprintln(stdout, "WARN: /dev/uinput missing:", err)
	} else {
		fmt.Fprintf(stdout, "ok: /dev/uinput (%s)\n", info.Mode())
	}
	if os.Getenv("DISPLAY") == "" {
		fmt.Fprintln(stdout, "WARN: DISPLAY is empty; text injection will fail")
	} else {
		fmt.Fprintln(stdout, "ok: DISPLAY", os.Getenv("DISPLAY"))
	}
	if remap.RemotePresent() {
		fmt.Fprintln(stdout, "ok: RC003 evdev device present")
	} else {
		fmt.Fprintln(stdout, "WARN: RC003 evdev device not found (bluetoothctl connect?)")
	}
	return failures
}
