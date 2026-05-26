package main

import (
	"context"
	"fmt"
	"os"

	taucli "github.com/samcharles93/tau/internal/cli"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	versionStr := fmt.Sprintf("%s (%s, %s)", version, commit, date)
	app := taucli.NewRootCommand(versionStr)
	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
