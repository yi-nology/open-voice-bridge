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

func Text(ctx context.Context, text string) error {
	if text == "" {
		return nil
	}
	return inject(ctx, text)
}
