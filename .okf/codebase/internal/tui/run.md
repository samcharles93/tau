---
description: Source module internal/tui/run.go (18 lines).
resource: internal/tui/run.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: run.go
type: Module
---

# Module run.go

**Path**: `internal/tui/run.go`  
**Lines**: 18

## Snippet Preview

```
package tui

import (
	"context"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/tui2"
)

// Run launches the interactive chat TUI. When cfg.NewTUI is true it delegates
// to the new Bubbletea-based frontend; otherwise it uses the legacy taui
// inline renderer. It blocks until the user exits.
func Run(ctx context.Context, runtime tauchat.ChatRuntime, cfg TUIConfig) error {
	if cfg.NewTUI {
		return tui2.Run(ctx, runtime, cfg.Bus, cfg.OnReady, cfg.SessionID, cfg.ModelName, cfg.Provider, cfg.MetricsConfig, cfg.AvailableModels, cfg.RefreshModels, cfg.ShowReasoning, cfg.ReasoningEffort, cfg.ToolCallsDefaultCollapsed, cfg.WebURL, cfg.Debug)
	}
	return RunInline(ctx, runtime, cfg)
}
```
