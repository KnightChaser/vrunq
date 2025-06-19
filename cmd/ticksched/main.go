package main

import (
	"context"
	"fmt"
	"time"

	"vrunq/internal/job"
	"vrunq/internal/sched"
)

func main() {
	// Read the configuration
	cfg := sched.Load("config.yml")
	fmt.Printf("Loaded config: %+v\n", cfg)
	scheduler := sched.New(cfg)

	// Start the new scheduler loop
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go scheduler.Loop(ctx)

	// helper to sleep N ticks
	sleepTicks := func(n int64) {
		time.Sleep(time.Duration(n*int64(cfg.TickMS)) * time.Millisecond)
	}

	// calculate ticks -> msec
	calculateTicks := func(ticks int64) int64 {
		return ticks * int64(cfg.TickMS)
	}

	// Task A @ tick 10, priority 3, total required ticks 5
	sleepTicks(10)
	taskA := sched.NewTask(1, 3, job.SleepWork(calculateTicks(5)))
	scheduler.Add(taskA)
	fmt.Printf("Added Task A (ID: %d, Priority: %d) at tick 10\n", taskA.ID, taskA.Priority)

	// Task B @ tick 14, priority 8, total required ticks 10
	sleepTicks(4)
	taskB := sched.NewTask(2, 8, job.SleepWork(calculateTicks(10)))
	scheduler.Add(taskB)
	fmt.Printf("Added Task B (ID: %d, Priority: %d) at tick 14\n", taskB.ID, taskB.Priority)

	// Task C @ tick 15, priority 10, total required ticks 8
	sleepTicks(1)
	taskC := sched.NewTask(3, 10, job.SleepWork(calculateTicks(8)))
	scheduler.Add(taskC)
	fmt.Printf("Added Task C (ID: %d, Priority: %d) at tick 15\n", taskC.ID, taskC.Priority)

	// let it run for a few seconds
	time.Sleep(5 * time.Second)

	// spin down
	cancel()
	fmt.Println("Scheduler stopped.")
}
