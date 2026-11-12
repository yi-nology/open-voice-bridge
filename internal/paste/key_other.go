//go:build !linux && !darwin

package paste

import "context"

func sendKey(ctx context.Context, combo string) error { return ErrUnsupported }
