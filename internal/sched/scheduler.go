// internal/sched/scheduler.go

package sched

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/emirpasic/gods/trees/redblacktree"
)

// Scheduler implements a mini CFS‑like scheduler and streams state changes.
type Scheduler struct {
	mu           sync.Mutex         // protects the scheduler state
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
		statusCh:     make(chan StatusEvent, 256),
	}
}

// StatusChannel exposes read‑only stream (optional consumers).
func (s *Scheduler) StatusChannel() <-chan StatusEvent { return s.statusCh }

// Run starts dispatch loop and prints every status event in unified format.
// A ticker goroutine generates StatusTick events each real-time tick so the
// global tick counter advances even while the run queue is empty.
func (s *Scheduler) Run(ctx context.Context) error {
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
	s.mu.Lock()

	if _, dup := s.tasks[t.ID]; dup {
		s.mu.Unlock()
		return fmt.Errorf("task %d already exists", t.ID)
	}

	t.Vruntime = s.minVruntime
	s.rbt.Put(nodeKey{t.Vruntime, t.ID}, t)
	s.tasks[t.ID] = t
	s.ranTotals[t.ID] = 0

	eventData := StatusEvent{
		Time:     time.Now(),
		Kind:     StatusEnqueue,
		TaskID:   t.ID,
		Vruntime: t.Vruntime}
	s.mu.Unlock() // NOTE: Unlock before sending to avoid deadlock if the channel is full
	s.statusCh <- eventData
	return nil
}

// AdjustPriority changes an existing task's priority on the fly.
// It reweights and requeues the task so future silces reflect the new priority.
func (s *Scheduler) AdjustPriority(id TaskID, newPriority int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("no such task %d", id)
	}

	if newPriority <= MinPriority {
		newPriority = MinPriority
	} else if newPriority >= MaxPriority {
		newPriority = MaxPriority
	}

	// Remove old tree entry, update fields, then reinsert the task
	// under the same vruntime. And emit an event so logs show the change.
	s.rbt.Remove(nodeKey{t.Vruntime, t.ID})
	t.Priority = newPriority
	t.Weight = float64(newPriority + 1)
	s.rbt.Put(nodeKey{
		vruntime: t.Vruntime,
		id:       t.ID,
	}, t)

	s.statusCh <- StatusEvent{
		Time:     time.Now(),
		Kind:     StatusPriorityUpdate,
		TaskID:   t.ID,
		Vruntime: t.Vruntime,
	}
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
		s.mu.Lock() // NOTE: protect the red-black tree and tasks map for stable reads
		node := s.rbt.Left()
		if node == nil {
			// No task, nap for a tick and continue.
			s.mu.Unlock() // NOTE: release the lock before sleeping to avoid deadlock
			time.Sleep(s.tickDuration)
			continue
		}

		// If we have tasks, we run the one with the lowest vruntime.
		key := node.Key.(nodeKey)
		t := node.Value.(*Task)
		s.rbt.Remove(key)
		s.minVruntime = key.vruntime
		s.mu.Unlock() // NOTE: release the lock before running the task
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
			// Ensure vruntime always advances at least by 1 tick.
			ranTicks = 1
		}

		s.mu.Lock() // NOTE: re-acquire the lock to update the task state
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

		// Update the minimum vruntime after running a task.
		if first := s.rbt.Left(); first != nil {
			s.minVruntime = first.Key.(nodeKey).vruntime
		}

		s.mu.Unlock() // NOTE: release the lock after updating the task state

		s.statusCh <- StatusEvent{
			Time:     time.Now(),
			Kind:     kind,
			TaskID:   t.ID,
			Vruntime: t.Vruntime,
			RanTicks: ranTicks}

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
		center(ev.Kind.String(), 16),
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
