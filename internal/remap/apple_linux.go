//go:build linux

package remap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Gen-1 Siri Remote (A1513/A1962): Apple vendor-defined HID report 0xFC.
const (
	appleHIDID           = "HID_ID=0005:0000004C:0000026D"
	appleHIDIOCSFeature2 = uintptr(3<<30 | 2<<16 | 'H'<<8 | 0x06)
)

var appleButtons = []struct {
	bit  byte
	name string
}{
	{0x01, "home"},
	{0x02, "volumeup"},
	{0x04, "volumedown"},
	{0x08, "playpause"},
	{0x10, "siri"},
	{0x20, "compose"},
	{0x80, "enter"},
}

func (d *Daemon) appleLoop(ctx context.Context) {
	for ctx.Err() == nil {
		if !d.appleActive() {
			if !sleepCtx(ctx, 2*time.Second) {
				return
			}
			continue
		}
		path := findAppleHID()
		if path == "" {
			if !sleepCtx(ctx, 2*time.Second) {
				return
			}
			continue
		}
		d.readApple(ctx, path)
		if !sleepCtx(ctx, time.Second) {
			return
		}
	}
}

func findAppleHID() string {
	matches, err := filepath.Glob("/sys/class/hidraw/hidraw*/device/uevent")
	if err != nil {
		return ""
	}
	for _, uevent := range matches {
		data, err := os.ReadFile(uevent)
		if err != nil {
			continue
		}
		if strings.Contains(string(data), appleHIDID) {
			node := filepath.Base(filepath.Dir(filepath.Dir(uevent)))
			return "/dev/" + node
		}
	}
	return ""
}

func (d *Daemon) readApple(ctx context.Context, path string) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		log.Warn("open apple remote", "path", path, "err", err)
		return
	}
	defer func() {
		if err := unix.Close(fd); err != nil {
			log.Debug("close apple hidraw", "err", err)
		}
	}()
	if err := unlockApple(fd); err != nil {
		log.Warn("unlock apple remote", "path", path, "err", err)
		return
	}
	log.Info("apple remote active", "path", path)
	pollFds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	buffer := make([]byte, 1024)
	var previous byte
	for ctx.Err() == nil {
		if !d.appleActive() {
			return
		}
		ready, err := unix.Poll(pollFds, 1000)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return
		}
		if ready == 0 {
			continue
		}
		n, err := unix.Read(fd, buffer)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return
		}
		if n < 3 || buffer[0] != 0xFC {
			continue
		}
		mask := buffer[2]
		d.applyAppleButtons(previous, mask)
		previous = mask
	}
}

func unlockApple(fd int) error {
	buffer := [2]byte{0xF0, 0xAF}
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), appleHIDIOCSFeature2, uintptr(unsafe.Pointer(&buffer[0])))
	if errno != 0 {
		return errno
	}
	return nil
}

type appleTransition struct {
	name  string
	value int
}

func appleTransitions(previous, current byte) []appleTransition {
	changed := previous ^ current
	var out []appleTransition
	for _, button := range appleButtons {
		if changed&button.bit == 0 {
			continue
		}
		value := 0
		if current&button.bit != 0 {
			value = 1
		}
		out = append(out, appleTransition{name: button.name, value: value})
	}
	return out
}

func (d *Daemon) applyAppleButtons(previous, current byte) {
	for _, transition := range appleTransitions(previous, current) {
		code, ok := SourceCodes[transition.name]
		if !ok {
			continue
		}
		d.applyKey(code, transition.name, transition.value)
	}
}
