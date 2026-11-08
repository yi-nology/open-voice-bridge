// Package paste injects recognized text into the focused application.
package paste

import (
	"context"
	oeerrors "github.com/oekit/pkg/errors"
)

var ErrUnsupported = oeerrors.New("text injection not implemented on this platform")

func Text(ctx context.Context, text string) error {
	if text == "" {
		return nil
	}
	return inject(ctx, text)
}
