package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	taucli "github.com/samcharles93/tau/internal/cli"
)

var (
	version = "dev"
	date    = "unknown"
)

func main() {
	os.Exit(run())
}

// run executes the CLI and returns the process exit code. Keeping os.Exit out
// of main's body lets the deferred cancel() actually run.
func run() int {
	versionStr := formatVersion(version, date, time.Now())
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	app := taucli.NewRootCommand(versionStr)
	if err := app.Run(ctx, os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}
