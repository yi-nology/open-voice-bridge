//go:build !linux && !darwin

package paste

import "context"

func injectBackend(ctx context.Context, text, backend string) error { return ErrUnsupported }
