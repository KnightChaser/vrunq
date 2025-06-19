// cmd/tickssched/main.go

package main

import (
	"context"
	"fmt"
	"time"

	"vrunq/internal/job"
	"vrunq/internal/sched"
)

func main() {
	// load config & build scheduler
	cfg := sched.Load("config.yml")
	fmt.Printf("Loaded config: %+v\n", cfg)
	scheduler := sched.New(cfg)

	// prepare cancelable context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// start the scheduler + printer
	go func() {
		if err := scheduler.Run(ctx); err != nil {
			fmt.Println("Scheduler exited with:", err)
		}
	}()

	// helpers to convert ticks <-> real time
	sleepTicks := func(n int64) {
		time.Sleep(time.Duration(n*int64(cfg.TickMS)) * time.Millisecond)
	}
	calcMS := func(ticks int64) int64 {
		return ticks * int64(cfg.TickMS)
	}

	// dynamically add tasks
	sleepTicks(10)
	scheduler.Add(sched.NewTask(1, 3, job.SleepWork(calcMS(5))))

	sleepTicks(4)
	scheduler.Add(sched.NewTask(2, 8, job.SleepWork(calcMS(10))))

	sleepTicks(1)
	scheduler.Add(sched.NewTask(3, 10, job.SleepWork(calcMS(8))))

	// let it run for a bit, then stop
	sleepTicks(100)
	cancel()

	time.Sleep(100 * time.Millisecond)
	fmt.Println("Scheduler stopped.")
}
