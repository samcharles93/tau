---
description: Source module cmd/tau/main.go (27 lines).
resource: cmd/tau/main.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: main.go
type: Module
---

# Module main.go

**Path**: `cmd/tau/main.go`  
**Lines**: 27

## Snippet Preview

```
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
```
