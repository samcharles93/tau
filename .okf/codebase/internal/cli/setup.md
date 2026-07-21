---
description: Source module internal/cli/setup.go (95 lines).
resource: internal/cli/setup.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: setup.go
type: Module
---

# Module setup.go

**Path**: `internal/cli/setup.go`  
**Lines**: 95

## Snippet Preview

```
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/samcharles93/tau/internal/app"
	urfavecli "github.com/urfave/cli/v3"
)

// cliSetupPrompter implements app.SetupPrompter using this package's
// terminal Select/ReadSecret primitives (see prompt.go), translating this
// package's cancellation sentinel into app's so RunSetup stays free of any
// terminal dependency.
type cliSetupPrompter struct{}

func (cliSetupPrompter) Select(ctx context.Context, title string, options []app.SetupOption) (app.SetupOption, error) {
	opts := make([]SelectOption, len(options))
	for i, o := range options {
		opts[i] = SelectOption{Label: o.Label, Value: o.Value}
	}
	choice, err := Select(ctx, os.Stdout, os.Stdin, title, opts)
	if err != nil {
		if errors.Is(err, ErrPromptCanceled) {
			return app.SetupOption{}, app.ErrSetupCanceled
		}
		return app.SetupOption{}, err
	}
```
