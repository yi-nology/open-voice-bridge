// Package paste injects recognized text into the focused application.
package paste

import (
	"context"
	"strings"

	oeerrors "github.com/oekit/pkg/errors"
)

var ErrUnsupported = oeerrors.New("text injection not implemented on this platform")

// Key sends one key combo ("ctrl+c", "enter") to the focused window. It is
// the execution arm of voice commands (语音命令).
func Key(ctx context.Context, combo string) error {
	combo = strings.TrimSpace(combo)
	if combo == "" {
		return oeerrors.New("empty key combo")
	}
	return sendKey(ctx, combo)
}

// Injection backends. "" and "auto" use the native Wayland protocol when it is
// available and fall back to the X11 clipboard; the explicit names force one
// path. Wayland is not always right: labwc's virtual-keyboard handling turned
// every character into a key from the number row (typing "AAA111" produced
// "11112341235"), so a host can pin the backend that works there.
const (
	BackendAuto    = "auto"
	BackendWayland = "wayland"
	BackendX11     = "x11"
)

// Injector resolves the configured backend once, so the hot path does not
// re-read the setting for every utterance.
func Injector(backend string) func(context.Context, string) error {
	switch NormalizeBackend(backend) {
	case BackendWayland, BackendX11:
		chosen := NormalizeBackend(backend)
		return func(ctx context.Context, text string) error {
			if text == "" {
				return nil
			}
			return injectBackend(ctx, text, chosen)
		}
	default:
		return Text
	}
}

// NormalizeBackend maps anything unrecognised to auto, which is what the
// config validator allows and what an old document carries.
func NormalizeBackend(backend string) string {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case BackendWayland:
		return BackendWayland
	case BackendX11:
		return BackendX11
	default:
		return BackendAuto
	}
}

func Text(ctx context.Context, text string) error {
	if text == "" {
		return nil
	}
	return injectBackend(ctx, text, BackendAuto)
}
