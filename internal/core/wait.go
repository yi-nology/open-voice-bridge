package core

import (
	"context"
	"time"

	"github.com/oekit/pkg/timex"
)

// Sleep waits for d, or until ctx ends. True means the full wait elapsed.
// The one bool-shaped face over oekit's timex.SleepContext (nil-safe ctx
// included); the copies three packages carried are gone.
func Sleep(ctx context.Context, d time.Duration) bool {
	return timex.SleepContext(ctx, d) == nil
}
