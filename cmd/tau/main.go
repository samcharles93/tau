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
	versionStr := formatVersion(version, date, time.Now())
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	app := taucli.NewRootCommand(versionStr)
	if err := app.Run(ctx, os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
