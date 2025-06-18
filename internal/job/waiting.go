package job

import (
	"context"
	"errors"
	"math/rand"
	"time"
)

// demowork simulates I/O: sleeps 50 ~ 150 ms or returns if ctx is done.
func demoWork(ctx context.Context) error {
	delay := time.Duration(50+rand.Intn(100)) * time.Millisecond
	select {
	case <-time.After(delay):
		// Simulate work done if not cancelled.
		return nil
	case <-ctx.Done():
		return errors.New("task canceled: " + ctx.Err().Error())
	}
}
