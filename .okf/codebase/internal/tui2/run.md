---
description: Source module internal/tui2/run.go (106 lines).
resource: internal/tui2/run.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: run.go
type: Module
---

# Module run.go

**Path**: `internal/tui2/run.go`  
**Lines**: 106

## Snippet Preview

```
// Package tui2 implements a new Bubbletea v2-based interactive chat TUI
// as a sibling to the legacy taui inline renderer (internal/tui). Both
// frontends share the same eventbus.Bus, agent.Coordinator, and plugin
// contracts - tui2 is just a different renderer.
//
// It is the default TUI; --legacy-tui falls back to the legacy renderer.
package tui2

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/metrics"
)

// Run launches the Bubbletea v2 chat TUI. It creates bus clients, subscribes
// to chat events, wires metrics tracking, and blocks until the user exits.
//
// Parameters are passed individually rather than via TUIConfig to avoid a
// circular import between internal/tui (which calls this function) and
// internal/tui2.
func Run(
```
