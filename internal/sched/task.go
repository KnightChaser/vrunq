package sched

import "context"

// TaskID uniquely identifies a task in the scheduler.
type TaskID uint64

// Task represents one schedulable unit.
type Task struct {
	ID       TaskID
	Priority int     // 0..40  (higher = nicer / lower weight)
	Weight   float64 // = float64(Priority+1)
	Vruntime float64 // CFS virtual runtime
	Run      func(context.Context) error
}

// NewTask builds a task; Vruntime is filled on enqueue.
func NewTask(id TaskID, priority int, fn func(context.Context) error) *Task {
	if priority < MinPriority {
		priority = MinPriority
	} else if priority > MaxPriority {
		priority = MaxPriority
	}
	return &Task{
		ID:       id,
		Priority: priority,
		Weight:   float64(priority + 1),
		Vruntime: 0,
		Run:      fn,
	}
}
