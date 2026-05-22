package main

import (
	"context"
	"fmt"
	"os"

	aimcli "bitbucket.srv.westpac.com.au/m055731/aim/internal/cli"
)

var version = "dev"

func main() {
	app := aimcli.NewRootCommand(version)
	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
