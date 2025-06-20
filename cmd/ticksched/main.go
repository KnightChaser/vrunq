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
	scheduler.EnableCSVLogging("scheduling.csv")

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
	sleepTicks(1)

	// 2) T2 @ tick1 (25 ticks at prio=2)
	scheduler.Add(sched.NewTask(2, 2, job.SleepWork(calcMS(25))))
	sleepTicks(1)

	// 3) T3 @ tick2 (20 ticks at prio=10)
	scheduler.Add(sched.NewTask(3, 10, job.SleepWork(calcMS(20))))
	sleepTicks(1)

	// 4) T4 @ tick3 (15 ticks at prio=7)
	scheduler.Add(sched.NewTask(4, 7, job.SleepWork(calcMS(15))))
	sleepTicks(1)

	// 5) T5 @ tick4 (10 ticks at prio=3)
	scheduler.Add(sched.NewTask(5, 3, job.SleepWork(calcMS(10))))
	sleepTicks(2)

	// Intermittently adjust priorities
	scheduler.AdjustPriority(3, 1)  // Demote T3
	sleepTicks(2)
	scheduler.AdjustPriority(2, 15) // Boost T2
	sleepTicks(2)
	scheduler.AdjustPriority(4, 2)  // Demote T4
	sleepTicks(2)
	scheduler.AdjustPriority(1, 12) // Boost T1
	sleepTicks(2)
	scheduler.AdjustPriority(5, 8)  // Boost T5

	// 5) Drain the rest
	sleepTicks(300)
	cancel()

	time.Sleep(100 * time.Millisecond)
	fmt.Println("Scheduler stopped.")
}
