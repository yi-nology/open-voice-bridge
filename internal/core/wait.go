package core

import (
	"context"
	"time"
)

// Sleep waits for d, or until ctx ends. True means the full wait elapsed.
// Three packages carried their own copy (two bool-shaped, one error-shaped);
// this is the one.
func Sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
