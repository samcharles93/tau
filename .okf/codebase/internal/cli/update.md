---
description: Source module internal/cli/update.go (71 lines).
resource: internal/cli/update.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: update.go
type: Module
---

# Module update.go

**Path**: `internal/cli/update.go`  
**Lines**: 71

## Snippet Preview

```
package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/samcharles93/tau/internal/updater"
	urfavecli "github.com/urfave/cli/v3"
)

func updateCmd(currentVersion string) *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "update",
		Usage: "Update tau to the latest GitHub release",
		Flags: []urfavecli.Flag{
			&urfavecli.BoolFlag{
				Name:  "check",
				Usage: "Check for an available update without installing it",
			},
			&urfavecli.StringFlag{
				Name:  "version",
				Usage: "Release tag to install (defaults to latest)",
			},
			&urfavecli.StringFlag{
				Name:  "repo",
				Usage: "GitHub repository to update from",
				Value: updater.DefaultRepo,
			},
			&urfavecli.BoolFlag{
```
