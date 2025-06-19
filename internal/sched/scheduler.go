// internal/sched/scheduler.go

package sched

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/emirpasic/gods/trees/redblacktree"
)

// Scheduler implements a mini CFS‑like scheduler and streams state changes.
type Scheduler struct {
	sliceTicks   int64              // number of ticks to run a task before preempting (minimum guaranteed time slice)
	tickDuration time.Duration      // duration of a single tick in real time
	minVruntime  float64            // minimum vruntime of all tasks in the run queue
	rbt          *redblacktree.Tree // red-black tree ordered by vruntime and task ID
	tasks        map[TaskID]*Task   // map of all tasks by ID
	statusCh     chan StatusEvent   // channel for status events
	tickCount    int64              // wall‑clock ticks since Run() started
	ranTotals    map[TaskID]int64   // cumulative ticks per task
}

func New(cfg Config) *Scheduler {
	return &Scheduler{
		sliceTicks:   int64(cfg.SliceTicks),
		tickDuration: time.Duration(cfg.TickMS) * time.Millisecond,
		rbt:          redblacktree.NewWith(cmp),
		tasks:        make(map[TaskID]*Task),
		ranTotals:    make(map[TaskID]int64),
	}
}

// StatusChannel exposes read‑only stream (optional consumers).
func (s *Scheduler) StatusChannel() <-chan StatusEvent { return s.statusCh }

// Run starts dispatch loop and prints every status event in unified format.
// A ticker goroutine generates StatusTick events each real-time tick so the
// global tick counter advances even while the run queue is empty.
func (s *Scheduler) Run(ctx context.Context) error {
	s.statusCh = make(chan StatusEvent, 256)

	// generate tick events
	go func() {
		ticker := time.NewTicker(s.tickDuration)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case t := <-ticker.C:
				s.statusCh <- StatusEvent{
					Time: t,
					Kind: StatusTick}
			}
		}
	}()

	// start core dispatch loop
	go s.loop(ctx)

	// consume and log events
	for ev := range s.statusCh {
		s.handleEvent(ev)
	}
	return nil
}

// Add enqueues a task and emits a StatusEnqueue event.
func (s *Scheduler) Add(t *Task) error {
	if _, dup := s.tasks[t.ID]; dup {
		return fmt.Errorf("task %d already exists", t.ID)
	}
	t.Vruntime = s.minVruntime
	s.rbt.Put(nodeKey{t.Vruntime, t.ID}, t)
	s.tasks[t.ID] = t
	s.ranTotals[t.ID] = 0
	s.statusCh <- StatusEvent{
		Time:     time.Now(),
		Kind:     StatusEnqueue,
		TaskID:   t.ID,
		Vruntime: t.Vruntime}
	return nil
}

// loop runs the main dispatch loop, which is responsible for selecting the next task
func (s *Scheduler) loop(ctx context.Context) {
	defer close(s.statusCh)

	for {
		if ctx.Err() != nil {
			return
		}

		// Check if we have any tasks to run.
		node := s.rbt.Left()
		if node == nil {
			// No task, nap for a tick and continue.
			time.Sleep(s.tickDuration)
			continue
		}

		// If we have tasks, we run the one with the lowest vruntime.
		key := node.Key.(nodeKey)
		t := node.Value.(*Task)
		s.rbt.Remove(key)
		s.statusCh <- StatusEvent{
			Time:     time.Now(),
			Kind:     StatusDispatch,
			TaskID:   t.ID,
			Vruntime: t.Vruntime}

		// run one slice
		runCtx, cancel := context.WithTimeout(ctx, time.Duration(s.sliceTicks)*s.tickDuration)
		start := time.Now()
		err := t.Run(runCtx)
		cancel()

		ranTicks := int64(time.Since(start) / s.tickDuration)
		if ranTicks <= 0 {
			// Evnt if the task did not run, we still need to update vruntime.
			ranTicks = s.sliceTicks
		}
		t.Vruntime += float64(ranTicks) / t.Weight
		s.ranTotals[t.ID] += ranTicks

		kind := StatusPreempt
		if err == nil {
			// If the task finished successfully, we remove it from the run queue.
			kind = StatusFinish
			delete(s.tasks, t.ID)
		} else {
			// The task was exited. We reinsert it into the run queue with updated vruntime,
			// because we consider it was preempted (more time was needed).
			s.rbt.Put(nodeKey{t.Vruntime, t.ID}, t)
		}
		s.statusCh <- StatusEvent{
			Time:     time.Now(),
			Kind:     kind,
			TaskID:   t.ID,
			Vruntime: t.Vruntime,
			RanTicks: ranTicks}

		// Update the minimum vruntime after running a task.
		if first := s.rbt.Left(); first != nil {
			s.minVruntime = first.Key.(nodeKey).vruntime
		}
	}
}

func (s *Scheduler) handleEvent(ev StatusEvent) {
	switch ev.Kind {
	case StatusTick:
		// advance wall‑clock tick counter but do not print
		s.tickCount++
		return
	case StatusPreempt, StatusFinish:
		// ranTotals already updated in loop; nothing extra
	}

	center := func(str string, width int) string {
		spaces := int(float64(width-len(str)) / 2)
		return strings.Repeat(" ", spaces) + str + strings.Repeat(" ", width-(spaces+len(str)))
	}

	fmt.Printf("%s = Tick: %07d [%s] => Task: %04d, Total ran: %04d ticks, vruntime=%07.4f\n",
		ev.Time.Format("Jan 02 15:04:05.000"),
		s.tickCount,
		center(ev.Kind.String(), 10),
		ev.TaskID,
		s.ranTotals[ev.TaskID],
		ev.Vruntime,
	)
}

// nodeKey is used as a key in the red-black tree.
type nodeKey struct {
	vruntime float64
	id       TaskID
}

// nodeKey implements the Comparable interface for red-black tree ordering.
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
