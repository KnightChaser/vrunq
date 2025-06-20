// internal/sched/tickclock.go

package sched

import (
	"sync/atomic"
	"time"
)

// TickClock emits ticks and counts them atomically.
type TickClock struct {
	Ch    chan struct{}
	count atomic.Int64
	stop  chan struct{}
}

// NewTickClock creates a clock but does not share it.
func NewTickClock(buffer int) *TickClock {
	return &TickClock{
		Ch:   make(chan struct{}, buffer),
		stop: make(chan struct{}),
	}
}

// Start begins emitting ticks at the given interval.
func (c *TickClock) Start(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c.count.Add(1)
				c.Ch <- struct{}{}
			case <-c.stop:
				close(c.Ch)
				return
			}
		}
	}()
}

// Stop signals the clock to stop emitting ticks.
func (c *TickClock) Stop() {
	close(c.stop)
}

// Count returns the current tick count atomically.
func (c *TickClock) Count() int64 {
	return c.count.Load()
}
