package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/samcharles93/tau/internal/app"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
)

func main() {
	// Create a minimal test environment
	bus := eventbus.New()
	defer bus.Close()

	// Create a test coordinator
	coord, err := app.NewCoordinator(context.Background(), app.CoordinatorOptions{
		WorkingDir: ".",
	}, "test-session", nil, nil, bus, nil, nil, nil, nil, false)
	if err != nil {
		fmt.Printf("Failed to create coordinator: %v\n", err)
		os.Exit(1)
	}
	defer coord.Close()

	// Send the ListSkillsCommand
	cmd := chat.ListSkillsCommand{RequestedAt: time.Now().UTC()}
	err = coord.Send(cmd)
	if err != nil {
		fmt.Printf("Failed to send command: %v\n", err)
		os.Exit(1)
	}

	// Give it a moment to process
	time.Sleep(100 * time.Millisecond)
	fmt.Println("Test completed. Check the output above for skill listing.")
}