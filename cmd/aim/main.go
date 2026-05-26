package main

import (
	"context"
	"fmt"
	"os"

	aimcli "bitbucket.srv.westpac.com.au/m055731/aim/internal/cli"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	versionStr := fmt.Sprintf("%s (%s, %s)", version, commit, date)
	app := aimcli.NewRootCommand(versionStr)
	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
