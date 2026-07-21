---
description: Source module internal/cli/commands.go (404 lines).
resource: internal/cli/commands.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: commands.go
type: Module
---

# Module commands.go

**Path**: `internal/cli/commands.go`  
**Lines**: 404

## Snippet Preview

```
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/samcharles93/tau/internal/app"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/providers"
	"github.com/samcharles93/tau/internal/sessions"
	"github.com/samcharles93/tau/internal/skills"
	"github.com/samcharles93/tau/internal/store"
	urfavecli "github.com/urfave/cli/v3"
)

var defaultChatParameters = tauchat.DefaultParameters()

func tokenCmd() *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "token",
		Usage: "Print the resolved provider bearer token to stdout",
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			_, selectedProvider, err := loadProvider(ctx, cmd)
			if err != nil {
				return err
```
