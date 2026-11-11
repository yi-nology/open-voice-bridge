//go:build linux

// Package focus reports the frontmost window class so keymap profiles can
// follow the active app (SayAll 切得准). X11 only: Wayland compositors do not
// expose focus to clients, so the switcher simply stays idle there.
package focus

import (
	"strings"
	"sync"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

var (
	mu   sync.Mutex
	conn *xgb.Conn
)

// ActiveClass returns the WM_CLASS res_class of the focused window. The X
// connection is kept open and rebuilt on the next call after a failure.
func ActiveClass() (string, error) {
	mu.Lock()
	defer mu.Unlock()
	if conn == nil {
		c, err := xgb.NewConn()
		if err != nil {
			return "", err
		}
		conn = c
	}
	class, err := activeClassLocked(conn)
	if err != nil {
		conn.Close()
		conn = nil
	}
	return class, err
}

func activeClassLocked(c *xgb.Conn) (string, error) {
	root := xproto.Setup(c).DefaultScreen(c).Root
	activeAtom, err := xproto.InternAtom(c, true, uint16(len("_NET_ACTIVE_WINDOW")), "_NET_ACTIVE_WINDOW").Reply()
	if err != nil {
		return "", err
	}
	windowProp, err := xproto.GetProperty(c, false, root, activeAtom.Atom, xproto.AtomWindow, 0, 1).Reply()
	if err != nil {
		return "", err
	}
	if len(windowProp.Value) < 4 {
		return "", nil // no focused window
	}
	window := xproto.Window(uint32(windowProp.Value[0]) | uint32(windowProp.Value[1])<<8 |
		uint32(windowProp.Value[2])<<16 | uint32(windowProp.Value[3])<<24)
	classAtom, err := xproto.InternAtom(c, true, uint16(len("WM_CLASS")), "WM_CLASS").Reply()
	if err != nil {
		return "", err
	}
	classProp, err := xproto.GetProperty(c, false, window, classAtom.Atom, xproto.AtomString, 0, 64).Reply()
	if err != nil {
		return "", err
	}
	// WM_CLASS is "res_name\0res_class\0"; the class is the second field.
	parts := strings.Split(string(classProp.Value), "\x00")
	if len(parts) < 2 {
		return strings.TrimSpace(parts[0]), nil
	}
	return strings.TrimSpace(parts[1]), nil
}
