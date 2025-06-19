// internal/sched/schedulerEvent.go

package sched

import (
	"time"
)

// StatusKind represents the type of scheduler event
type StatusKind int

const (
	StatusIdle StatusKind = iota
	StatusEnqueue
	StatusDispatch
	StatusPreempt
	StatusFinish
	StatusTick
)

// StatusEvent is emitted every tick or on key actions
type StatusEvent struct {
	Time     time.Time
	Kind     StatusKind
	TaskID   TaskID
	Vruntime float64
	RanTicks int64
}

func (sk StatusKind) String() string {
	switch sk {
	case StatusIdle:
		return "Idle"
	case StatusEnqueue:
		return "Enqueued"
	case StatusDispatch:
		return "Dispatch"
	case StatusPreempt:
		return "Preempt"
	case StatusFinish:
		return "Finish"
	case StatusTick:
		return "Tick"
	default:
		return "Unknown"
	}
}
