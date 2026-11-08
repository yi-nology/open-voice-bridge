//go:build !linux

package paste

import "context"

func inject(ctx context.Context, text string) error { return ErrUnsupported }
