package dify

import (
	"context"
	"time"
)

// sleepCtx waits, or stops waiting when the context ends. In the kernel
// because both the transport's backoff and a document's indexing poll wait
// this way.
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
