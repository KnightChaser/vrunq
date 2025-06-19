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
	scheduler.EnableCSVLogging("events.csv")

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

	// 1) T1 @ tick0 (30 ticks of work at prio=5)
	scheduler.Add(sched.NewTask(1, 5, job.SleepWork(calcMS(30))))
	sleepTicks(5)

	// 2) T2 @ tick5 (25 ticks at prio=2)
	scheduler.Add(sched.NewTask(2, 2, job.SleepWork(calcMS(25))))
	sleepTicks(3)

	// 3) T3 @ tick8 (20 ticks at prio=10 → then demote to prio=1)
	scheduler.Add(sched.NewTask(3, 10, job.SleepWork(calcMS(20))))
	scheduler.AdjustPriority(3, 1)
	sleepTicks(5)

	//  4. After T2’s first preempt (should happen at tick ~10), boost T2
	//     You can approximate: wait another 5 ticks
	sleepTicks(5)
	scheduler.AdjustPriority(2, 20)

	// 5) Drain the rest
	sleepTicks(100)
	cancel()

	time.Sleep(100 * time.Millisecond)
	fmt.Println("Scheduler stopped.")
}
