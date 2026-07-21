---
description: Source module internal/tui/run_taui.go (66 lines).
resource: internal/tui/run_taui.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: run_taui.go
type: Module
---

# Module run_taui.go

**Path**: `internal/tui/run_taui.go`  
**Lines**: 66

## Snippet Preview

```
package tui

import (
	"context"
	"fmt"
	"log/slog"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/metrics"
	"github.com/samcharles93/tau/pkg/taui"
)

// RunInline launches the chat TUI using taui's inline renderer.
func RunInline(ctx context.Context, runtime tauchat.ChatRuntime, cfg TUIConfig) error {
	if cfg.Bus == nil {
		return fmt.Errorf("event bus is required for TUI")
	}

	tuiClient := cfg.Bus.Client("tui")
	defer tuiClient.Close()

	chatSub := eventbus.Subscribe[tauchat.ChatEvent](tuiClient)
	defer chatSub.Close()

	// Subscribing the usage tracker to chat.MetricEvent is what activates the
	// coordinator's metric emission (it's a no-op until something subscribes).
	metricsClient := cfg.Bus.Client("tui-metrics")
	defer metricsClient.Close()
	tracker := metrics.NewUsageTracker(metricsClient)
```
