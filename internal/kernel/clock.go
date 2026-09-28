package kernel

import (
	"context"
	"time"
)

// SleepCtx waits, or stops waiting when the context ends. In the kernel
// because both the transport's backoff and a document's indexing poll wait
// this way.
func SleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
