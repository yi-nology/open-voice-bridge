//go:build linux

package paste

import (
	"context"
	oeerrors "github.com/oekit/pkg/errors"
	"os/exec"
	"strings"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

func injectX11(ctx context.Context, text string) error {
	if _, err := exec.LookPath("xsel"); err != nil {
		return oeerrors.Errorf("xsel not installed: %w", err)
	}
	set := exec.CommandContext(ctx, "xsel", "--clipboard", "--input")
	set.Stdin = strings.NewReader(text)
	if err := set.Run(); err != nil {
		return oeerrors.Errorf("set clipboard: %w", err)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(80 * time.Millisecond):
	}

	conn, err := xgb.NewConn()
	if err != nil {
		return oeerrors.Errorf("connect X display: %w", err)
	}
	defer conn.Close()
	if err := xtest.Init(conn); err != nil {
		return oeerrors.Errorf("init XTEST: %w", err)
	}
	setup := xproto.Setup(conn)
	screen := setup.DefaultScreen(conn)
	min := setup.MinKeycode
	count := byte(setup.MaxKeycode - setup.MinKeycode + 1)
	reply, err := xproto.GetKeyboardMapping(conn, min, count).Reply()
	if err != nil {
		return oeerrors.Errorf("keyboard mapping: %w", err)
	}
	per := int(reply.KeysymsPerKeycode)
	if per <= 0 {
		return oeerrors.Errorf("invalid keyboard mapping")
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
		return oeerrors.Errorf("Control_L keycode not found")
	}
	keyV, ok := find(0x0076) // XK_v
	if !ok {
		return oeerrors.Errorf("v keycode not found")
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
			return oeerrors.Errorf("XTEST fake input: %w", err)
		}
	}
	conn.Sync()
	return nil
}
