package job

import (
	"context"
	"time"
)

// Demowork returns a runnable that just sleeps for the given duration.
func SleepWork(ms int64) func(context.Context) error {
	remaining := time.Duration(ms) * time.Millisecond
	return func(ctx context.Context) error {
		start := time.Now()
		select {
		case <-ctx.Done():
			remaining -= time.Since(start)
			if remaining < 0 {
				remaining = 0
			}
			return ctx.Err()
		case <-time.After(remaining):
			// If the time is up, we just return nil.
			return nil
		}
	}
}
