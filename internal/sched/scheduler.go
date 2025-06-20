// internal/sched/scheduler.go

package sched

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emirpasic/gods/trees/redblacktree"
)

// Scheduler implements a mini CFS‑like scheduler and streams state changes.
type Scheduler struct {
	// Scheduler-related
	mu          sync.Mutex         // protects the scheduler state
	sliceTicks  int64              // number of ticks to run a task before preempting (minimum guaranteed time slice)
	clock       *TickClock         // clock for generating ticks
	minVruntime float64            // minimum vruntime of all tasks in the run queue
	rbt         *redblacktree.Tree // red-black tree ordered by vruntime and task ID
	tasks       map[TaskID]*Task   // map of all tasks by ID
	statusCh    chan StatusEvent   // channel for status events
	ranTotals   map[TaskID]int64   // cumulative ticks per task

	// logging-related
	csvFile   *os.File
	csvWriter *csv.Writer
}

// New creates a new Scheduler instance with the given configuration.
func New(cfg Config) *Scheduler {
	clock := NewTickClock(256) // buffer size for tick events
	clock.Start(time.Duration(cfg.TickMS) * time.Millisecond)

	return &Scheduler{
		sliceTicks: int64(cfg.SliceTicks),
		clock:      clock,
		rbt:        redblacktree.NewWith(cmp),
		tasks:      make(map[TaskID]*Task),
		ranTotals:  make(map[TaskID]int64),
		statusCh:   make(chan StatusEvent, 256), // buffered channel for status events
	}
}

// EnableCSVLogging opens the given file path for CSV logging of events.
// Must be called before Run().
func (s *Scheduler) EnableCSVLogging(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)

	// write header
	w.Write([]string{"timestamp", "tick", "event", "task_id", "ran_ticks", "vruntime"})
	w.Flush()
	s.csvFile = f
	s.csvWriter = w
	return nil
}

// StatusChannel exposes read‑only stream (optional consumers).
func (s *Scheduler) StatusChannel() <-chan StatusEvent { return s.statusCh }

func (s *Scheduler) Run(ctx context.Context) error {
	// start loop
	go s.loop(ctx)

	// consume events
	for ev := range s.statusCh {
		s.handleEvent(ev)
	}

	if s.csvFile != nil {
		s.csvWriter.Flush()
		s.csvFile.Close()
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
	defer func() {
		// stop the underlyinig clock to release its goroutine
		s.clock.Stop()
		close(s.statusCh)
	}()

	for {
		// 1) check shutdown
		if ctx.Err() != nil {
			return
		}

		// 2) idle case: if no tasks in queue, still emit a tick
		s.mu.Lock()
		node := s.rbt.Left()
		if node == nil {
			s.mu.Unlock()
			// drive one tick
			<-s.clock.Ch
			s.statusCh <- StatusEvent{
				Time: time.Now(),
				Kind: StatusTick,
			}
			continue
		}

		// 3) dispatch next task
		key := node.Key.(nodeKey)
		t := node.Value.(*Task)
		s.rbt.Remove(key)
		s.minVruntime = key.vruntime
		s.mu.Unlock()

		// emit dispatch event
		s.statusCh <- StatusEvent{
			Time:     time.Now(),
			Kind:     StatusDispatch,
			TaskID:   t.ID,
			Vruntime: t.Vruntime,
		}

		// 4) run exactly sliceTicks
		startTick := s.clock.Count()
		runCtx, cancel := context.WithCancel(ctx)
		// watcher: cancel the dispatched context(task) after sliceTicks
		go func() {
			for i := int64(0); i < s.sliceTicks; i++ {
				<-s.clock.Ch
				s.statusCh <- StatusEvent{
					Time: time.Now(),
					Kind: StatusTick,
				}
			}
			cancel() // cancel the run context after sliceTicks
		}()

		// While the watcher above is running, we can run the dispatched task.
		err := t.Run(runCtx)
		cancel()

		// how many ticks did we really run?
		ranTicks := s.clock.Count() - startTick
		if ranTicks <= 0 {
			ranTicks = 1
		}

		// 5) update vruntime + requeue or finish
		//    - If the task is preempted, the task exits with an error.
		//    - If the task finishes voluntarily, it returns nil.
		s.mu.Lock()
		t.Vruntime += float64(ranTicks) / t.Weight
		s.ranTotals[t.ID] += ranTicks

		kind := StatusPreempt
		if err == nil {
			kind = StatusFinish
			delete(s.tasks, t.ID)
		} else {
			s.rbt.Put(nodeKey{
				vruntime: t.Vruntime,
				id:       t.ID,
			}, t)
		}

		// also, update the minimum vruntime in the rbtree after requeueing
		if first := s.rbt.Left(); first != nil {
			s.minVruntime = first.Key.(nodeKey).vruntime
		}
		s.mu.Unlock()

		// 6) emit final event
		s.statusCh <- StatusEvent{
			Time:     time.Now(),
			Kind:     kind,
			TaskID:   t.ID,
			Vruntime: t.Vruntime,
			RanTicks: ranTicks,
		}
	}
}

func (s *Scheduler) handleEvent(ev StatusEvent) {
	// if we received a tick event which periodically occurs,
	// we can just return early and not log it for the brevity of output.
	if ev.Kind == StatusTick {
		return
	}

	// an auxiliary function to center the event kind in the output
	center := func(str string, width int) string {
		spaces := int(float64(width-len(str)) / 2)
		return strings.Repeat(" ", spaces) + str + strings.Repeat(" ", width-(spaces+len(str)))
	}

	msg := fmt.Sprintf("%s = Tick: %07d [%s] => Task: %04d, Total ran: %04d ticks, vruntime=%07.4f",
		ev.Time.Format("Jan 02 15:04:05.000"),
		s.clock.Count(),
		center(ev.Kind.String(), 16),
		ev.TaskID,
		s.ranTotals[ev.TaskID],
		ev.Vruntime,
	)
	fmt.Println(msg)

	// CSV output
	if s.csvWriter != nil {
		rec := []string{
			ev.Time.Format(time.RFC3339Nano),
			strconv.FormatInt(s.clock.Count(), 10),
			ev.Kind.String(),
			strconv.FormatInt(int64(ev.TaskID), 10),
			strconv.FormatInt(ev.RanTicks, 10),
			fmt.Sprintf("%.4f", ev.Vruntime),
		}
		s.csvWriter.Write(rec)
		s.csvWriter.Flush()
	}
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
