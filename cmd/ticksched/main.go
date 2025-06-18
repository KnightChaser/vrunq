package main

import (
	"context"
	"fmt"

	"vrunq/internal/sched"
)

func main() {
	// Read the configuration
	cfg := sched.Load("config.yml")
	fmt.Printf("Loaded config: %+v\n", cfg)

	// Create a new task
	task := sched.NewTask(1, 10, func(ctx context.Context) error {
		fmt.Println("Running task with ID 1")
		return nil
	})
	fmt.Printf("Created task: %+v\n", task)
}
