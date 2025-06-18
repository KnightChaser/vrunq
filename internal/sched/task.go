package sched

import "context"

// TaskID uniquely identifies a task in the scheduler.
type TaskID uint64

// Task represents one schedulable task unit.
type Task struct {
	ID       TaskID
	Priority int                             // 0 - 40, where 0 is the highest priority
	Weight   float64                         // computed as 1 + Task.Priority
	V        float64                         // stored virtual time
	TIn      int64                           // tick when (re)queued or entered the scheduler queue for the first time
	Run      func(ctx context.Context) error // work function (any kind, e.g. HTTP handler, DB txn stub, etc.)
}

// NewTask creates a new task with proper weight and zeroed V.
// NOTE: TIn and V are set to zero in here. These will be set when the task is queued.
func NewTask(id TaskID, priority int, work func(ctx context.Context) error) *Task {
	// clamp priority within the legal region.
	if priority < MinPriority {
		priority = MinPriority
	} else if priority > MaxPriority {
		priority = MaxPriority
	}

	return &Task{
		ID:       id,
		Priority: priority,
		Weight:   float64(1 + priority),
		V:        0,
		TIn:      0,
		Run:      work,
	}
}
