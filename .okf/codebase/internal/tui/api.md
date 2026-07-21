---
description: Source module internal/tui/api.go (49 lines).
resource: internal/tui/api.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: api.go
type: Module
---

# Module api.go

**Path**: `internal/tui/api.go`  
**Lines**: 49

## Snippet Preview

```
package tui

import (
	"context"

	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
)

// ModelRefresher is a function the app layer provides that re-discovers
// available models from the configured provider. The TUI calls it asynchronously when the
// user runs /refresh. This keeps the TUI decoupled from provider
// packages (per dependency rules).
type ModelRefresher func(ctx context.Context) ([]tauchat.ChatModelRef, error)

// TUIConfig holds parameters for constructing the TUI model.
type TUIConfig struct {
	SessionID          string
	ModelName          string
	Provider           string
	AvailableModels    []tauchat.ChatModelRef
	AvailableProviders []string
	// InitialCommands is the command registry snapshot at startup.
	// The registry owns command state; bus events deliver deltas.
	InitialCommands []tauchat.CommandRef
	Bus             *eventbus.Bus
	RefreshModels   ModelRefresher
	ShowReasoning   bool
	ReasoningEffort string
```
