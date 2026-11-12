//go:build linux

package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/core"
	"github.com/yi-nology/open-voice-bridge/internal/helper"
	"github.com/yi-nology/open-voice-bridge/internal/remap"
)

// hostChecks reports the Linux host prerequisites: root remap helper,
// PipeWire/X11 tooling, systemd units, uinput and the evdev remote. Every gap
// carries the command that resolves it (OPS-04).
func hostChecks(cfg config.Config) []check {
	checks := helperCheck(cfg)
	checks = append(checks, binaryChecks()...)
	checks = append(checks, unitChecks()...)
	checks = append(checks, lingerCheck(), uinputCheck(), sessionCheck())
	checks = append(checks, remoteChecks(cfg)...)
	return checks
}

func helperCheck(cfg config.Config) []check {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	status, err := helper.New(cfg.HelperSocket).Call(ctx, map[string]string{"cmd": "status"})
	if err != nil {
		return []check{gap(core.DepHelper, "remap helper: "+err.Error(),
			"sudo systemctl start xiaomi-remote-remap.service")}
	}
	return []check{ok(fmt.Sprintf("remap helper (active=%v, remote_present=%v)",
		status["keyd_active"], status["remote_present"]))}
}

// binaryChecks covers the external tools the agent shells out to. Each is an
// optional dependency: a phone-only setup needs none of the PipeWire pair, and
// Wayland injects text through wtype rather than xsel.
func binaryChecks() []check {
	tools := []struct {
		binary string
		dep    core.Dependency
		fix    string
	}{
		{"pw-cat", core.DepMic, "sudo apt install pipewire-bin"},
		{"pw-loopback", core.DepMic, "sudo apt install pipewire-bin"},
		{"xsel", core.DepPaste, "sudo apt install xsel"},
		{"bluetoothctl", core.DepBLE, "sudo apt install bluez"},
	}
	var checks []check
	for _, tool := range tools {
		if _, err := exec.LookPath(tool.binary); err != nil {
			checks = append(checks, gap(tool.dep, "missing "+tool.binary, tool.fix))
			continue
		}
		checks = append(checks, ok(tool.binary))
	}
	return checks
}

func unitChecks() []check {
	units := []struct {
		label string
		args  []string
		fix   string
	}{
		{"agent active", []string{"--user", "is-active", "xiaomi-remote-go"}, "systemctl --user start xiaomi-remote-go.service"},
		{"agent autostart", []string{"--user", "is-enabled", "xiaomi-remote-go"}, "systemctl --user enable xiaomi-remote-go.service"},
		{"remap active", []string{"is-active", "xiaomi-remote-remap"}, "sudo systemctl start xiaomi-remote-remap.service"},
		{"remap autostart", []string{"is-enabled", "xiaomi-remote-remap"}, "sudo systemctl enable xiaomi-remote-remap.service"},
	}
	var checks []check
	for _, unit := range units {
		out, err := exec.Command("systemctl", unit.args...).Output()
		state := strings.TrimSpace(string(out))
		if err != nil {
			checks = append(checks, warn(unit.label+": "+state, unit.fix))
			continue
		}
		checks = append(checks, ok(unit.label+": "+state))
	}
	return checks
}

func lingerCheck() check {
	current, err := user.Current()
	if err != nil {
		return warn("linger: "+err.Error(), "sudo loginctl enable-linger $USER")
	}
	out, err := exec.Command("loginctl", "show-user", current.Username, "-p", "Linger").Output()
	if err == nil && strings.Contains(string(out), "Linger=yes") {
		return ok("linger enabled (starts before login)")
	}
	return warn("linger disabled; agent will not start until you log in", "sudo loginctl enable-linger $USER")
}

func uinputCheck() check {
	info, err := os.Stat("/dev/uinput")
	if err != nil {
		return warn("/dev/uinput missing: "+err.Error(), "sudo modprobe uinput")
	}
	return ok(fmt.Sprintf("/dev/uinput (%s)", info.Mode()))
}

// sessionCheck reports the graphical session text injection needs. The user
// unit hardcodes DISPLAY, so a session that never exported it is the common
// cause of "recognition works but no text appears".
func sessionCheck() check {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return warn("no graphical session (DISPLAY and WAYLAND_DISPLAY are empty); text injection will fail",
			"systemctl --user import-environment DISPLAY WAYLAND_DISPLAY XAUTHORITY")
	}
	return ok(fmt.Sprintf("session DISPLAY=%q WAYLAND_DISPLAY=%q", os.Getenv("DISPLAY"), os.Getenv("WAYLAND_DISPLAY")))
}

func remoteChecks(cfg config.Config) []check {
	paths := remap.RemoteEventPaths()
	if len(paths) == 0 {
		return []check{warn("RC003 evdev device not found", "bluetoothctl connect "+cfg.Address)}
	}
	checks := []check{ok("RC003 evdev device present")}
	return append(checks, evdevReadChecks(paths)...)
}

// evdevReadChecks reports whether the agent's user can actually read the input
// nodes. Presence is not permission: /dev/input/eventN is root:input 0660, so a
// user outside the input group sees the device and still gets EACCES — the
// silent voice-key failure OPS-05 logs at runtime.
func evdevReadChecks(paths []string) []check {
	var checks []check
	for _, path := range paths {
		file, err := os.Open(path)
		if err == nil {
			_ = file.Close()
			checks = append(checks, ok("evdev readable: "+path))
			continue
		}
		fix := "ls -l " + path
		if errors.Is(err, os.ErrPermission) {
			fix = `sudo usermod -aG input "$USER", then log out and back in`
		}
		checks = append(checks, gap(core.DepRemote, "cannot read "+path+": "+err.Error(), fix))
	}
	return checks
}
