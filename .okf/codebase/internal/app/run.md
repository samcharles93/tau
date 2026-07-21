---
description: Source module internal/app/run.go (507 lines).
resource: internal/app/run.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: run.go
type: Module
---

# Module run.go

**Path**: `internal/app/run.go`  
**Lines**: 507

## Snippet Preview

```
package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/samcharles93/tau/internal/agent"
	"github.com/samcharles93/tau/internal/agent/tools"

	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	commandreg "github.com/samcharles93/tau/internal/registry"
	"github.com/samcharles93/tau/internal/sessions"
	"github.com/samcharles93/tau/internal/skills"
	"github.com/samcharles93/tau/internal/tui"
)

// startupLog is a dedicated file logger for startup diagnostics.
// slog output goes to stderr which would corrupt the TUI, so we write
// diagnostics to a separate file under the tau config directory.
var startupLog *slog.Logger

```
