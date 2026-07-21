---
description: Source module internal/app/stdin.go (275 lines).
resource: internal/app/stdin.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: stdin.go
type: Module
---

# Module stdin.go

**Path**: `internal/app/stdin.go`  
**Lines**: 275

## Snippet Preview

```
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/indexing"
	"github.com/samcharles93/tau/internal/metrics"
	"github.com/samcharles93/tau/internal/sessions"
	"github.com/samcharles93/tau/internal/skills"
)

const stdInTimeout = 60 * time.Minute

// RunStdIn processes a prompt in non-interactive mode and exits.
func RunStdIn(ctx context.Context, opts ChatOptions, prompt string) error {
	ctx, cancel := context.WithTimeout(ctx, stdInTimeout)
	defer cancel()

	rt := newRuntimeForProvider(opts.Provider, opts.Insecure)

```
