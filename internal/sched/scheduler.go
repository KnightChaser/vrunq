package sched

import (
	"context"
	"fmt"
	"time"

	"github.com/emirpasic/gods/trees/redblacktree"
)

type nodeKey struct {
	vruntime float64 // vruntime
	id       TaskID  // tie-breaker to keep keys unique
}

// Compare two nodeKeys by vruntime, then by ID (to keep them unique).
func cmp(a, b any) int {
	ka, kb := a.(nodeKey), b.(nodeKey)
	switch {
	case ka.vruntime < kb.vruntime:
		return -1
	case ka.vruntime > kb.vruntime:
		return 1
	case ka.id < kb.id:
		return -1
	case ka.id > kb.id:
		return 1
	default:
		return 0
	}
}

type Scheduler struct {
	sliceTicks   int64              // ticks per time-slice
	tickDuration time.Duration      // real duration of one tick
	minVruntime  float64            // global floor (CFS rule)
	rbt          *redblacktree.Tree // red-black tree of tasks, ordered by vruntime
	tasks        map[TaskID]*Task   // all tasks by ID
}

// New creates a new Scheduler with the given configuration.
func New(cfg Config) *Scheduler {
	return &Scheduler{
		sliceTicks:   int64(cfg.SliceTicks),
		tickDuration: time.Duration(cfg.TickMS) * time.Millisecond,
		minVruntime:  0,
		rbt:          redblacktree.NewWith(cmp),
		tasks:        make(map[TaskID]*Task),
	}
}

// Add enqueues a new task into the scheduler.
func (s *Scheduler) Add(t *Task) error {
	if _, dup := s.tasks[t.ID]; dup {
		return fmt.Errorf("task %d already exists", t.ID)
	}
	t.Vruntime = s.minVruntime // start at global minimum
	s.rbt.Put(nodeKey{t.Vruntime, t.ID}, t)
	s.tasks[t.ID] = t
	fmt.Printf("enqueue  Task %d  prio=%2d  vruntime=%.4f\n", t.ID, t.Priority, t.Vruntime)
	return nil
}

// Loop runs the scheduler loop, dispatching tasks based on their vruntime.
func (s *Scheduler) Loop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		node := s.rbt.Left()
		if node == nil {
			// no tasks; sleep a tick and poll again
			time.Sleep(s.tickDuration)
			continue
		}

		key := node.Key.(nodeKey)
		t := node.Value.(*Task)
		s.rbt.Remove(key)

		fmt.Printf("dispatch Task %d  vruntime=%.4f  slice=%d ticks\n",
			t.ID, t.Vruntime, s.sliceTicks)

		// run for one slice
		runCtx, cancel := context.WithTimeout(ctx, time.Duration(s.sliceTicks)*s.tickDuration)
		start := time.Now()
		err := t.Run(runCtx)
		cancel()

		// measure actual ticks used
		elapsed := time.Since(start)
		ranTicks := int64(elapsed / s.tickDuration)
		if ranTicks <= 0 {
			ranTicks = s.sliceTicks
		}

		// update vruntime: vr += ran / weight
		t.Vruntime += float64(ranTicks) / t.Weight

		if err == nil {
			// finished within slice
			delete(s.tasks, t.ID)
			fmt.Printf("finish   Task %d  ran=%d ticks  vruntime=%.4f\n",
				t.ID, ranTicks, t.Vruntime)
		} else {
			// pre-empted (context deadline exceeded)
			fmt.Printf("preempt  Task %d  ran=%d ticks  vruntime=%.4f\n",
				t.ID, ranTicks, t.Vruntime)
			s.rbt.Put(nodeKey{t.Vruntime, t.ID}, t)
		}

		// refresh global minimum
		if first := s.rbt.Left(); first != nil {
			s.minVruntime = first.Key.(nodeKey).vruntime
		}
	}
}
