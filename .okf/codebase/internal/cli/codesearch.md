---
description: Source module internal/cli/codesearch.go (52 lines).
resource: internal/cli/codesearch.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: codesearch.go
type: Module
---

# Module codesearch.go

**Path**: `internal/cli/codesearch.go`  
**Lines**: 52

## Snippet Preview

```
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/samcharles93/tau/internal/indexing"
	urfavecli "github.com/urfave/cli/v3"
)

func workspaceCodesearchCmd() *urfavecli.Command {
	return &urfavecli.Command{
		Name:   "workspace-codesearch",
		Hidden: true,
		Commands: []*urfavecli.Command{
			{
				Name: "build",
				Flags: []urfavecli.Flag{
					&urfavecli.StringFlag{Name: "root", Required: true},
					&urfavecli.StringFlag{Name: "index", Required: true},
				},
				Action: func(ctx context.Context, cmd *urfavecli.Command) error {
					if err := indexing.BuildIndex(ctx, cmd.String("root"), cmd.String("index")); err != nil {
						return fmt.Errorf("build workspace index: %w", err)
					}
					return nil
				},
			},
```
