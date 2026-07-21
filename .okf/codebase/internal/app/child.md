---
description: Source module internal/app/child.go (325 lines).
resource: internal/app/child.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: child.go
type: Module
---

# Module child.go

**Path**: `internal/app/child.go`  
**Lines**: 325

## Snippet Preview

```
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/samcharles93/tau/internal/agent/stdio"
	"github.com/samcharles93/tau/internal/bridge"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/sessions"
	"github.com/samcharles93/tau/internal/skills"
)

// RunChild is the headless child entry point. It writes agent.ready first
// (before reading anything - see step 1 below), reads agent.assign, loads
// its instance and session from the shared store, runs the coordinator
// headless with injected model/tools/limits, and exits after writing
// agent.result on stdout.
// stderr is reserved for log messages only - never protocol.
// Exit codes: 0 after result; 1 for protocol errors; 2 for fatal runtime errors.
func RunChild(ctx context.Context, opts ChatOptions) error {
```
