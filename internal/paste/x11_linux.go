//go:build linux

package paste

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
	oeerrors "github.com/oekit/pkg/errors"
)

func injectX11(ctx context.Context, text string) error {
	if _, err := exec.LookPath("xsel"); err != nil {
		return oeerrors.Wrap(err, "xsel not installed")
	}
	set := exec.CommandContext(ctx, "xsel", "--clipboard", "--input")
	set.Stdin = strings.NewReader(text)
	if err := set.Run(); err != nil {
		return oeerrors.Wrap(err, "set clipboard")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(80 * time.Millisecond):
	}

	conn, err := xgb.NewConn()
	if err != nil {
		return oeerrors.Wrap(err, "connect X display")
	}
	defer conn.Close()
	if err := xtest.Init(conn); err != nil {
		return oeerrors.Wrap(err, "init XTEST")
	}
	setup := xproto.Setup(conn)
	screen := setup.DefaultScreen(conn)
	min := setup.MinKeycode
	count := byte(setup.MaxKeycode - setup.MinKeycode + 1)
	reply, err := xproto.GetKeyboardMapping(conn, min, count).Reply()
	if err != nil {
		return oeerrors.Wrap(err, "keyboard mapping")
	}
	per := int(reply.KeysymsPerKeycode)
	if per <= 0 {
		return fmt.Errorf("invalid keyboard mapping")
	}
	find := func(keysym xproto.Keysym) (byte, bool) {
		for index, candidate := range reply.Keysyms {
			if candidate == keysym {
				return byte(min) + byte(index/per), true
			}
		}
		return 0, false
	}
	ctrl, ok := find(0xffe3) // XK_Control_L
	if !ok {
		return fmt.Errorf("Control_L keycode not found")
	}
	keyV, ok := find(0x0076) // XK_v
	if !ok {
		return fmt.Errorf("v keycode not found")
	}
	for _, event := range []struct {
		kind byte
		code byte
	}{
		{xproto.KeyPress, ctrl},
		{xproto.KeyPress, keyV},
		{xproto.KeyRelease, keyV},
		{xproto.KeyRelease, ctrl},
	} {
		if err := xtest.FakeInputChecked(conn, event.kind, event.code, 0, screen.Root, 0, 0, 0).Check(); err != nil {
			return oeerrors.Wrap(err, "XTEST fake input")
		}
	}
	conn.Sync()
	return nil
}
