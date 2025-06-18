package main

import (
	"fmt"

	"vrunq/internal/sched"
)

func main() {
	cfg := sched.Load("config.yml")
	fmt.Printf("Loaded config: %+v\n", cfg)

	// TODO: continued...
}
