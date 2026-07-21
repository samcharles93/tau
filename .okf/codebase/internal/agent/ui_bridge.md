---
description: Source module internal/agent/ui_bridge.go (222 lines).
resource: internal/agent/ui_bridge.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: ui_bridge.go
type: Module
---

# Module ui_bridge.go

**Path**: `internal/agent/ui_bridge.go`  
**Lines**: 222

## Snippet Preview

```
package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/pkg/plugin/api"
)

type interactivePromptResponse struct {
	confirmed bool
	canceled  bool
	response  string
}

type coordinatorUIBridge struct {
	coordinator *Coordinator
}

// SessionID returns "" - this bridge is shared across every session the
// coordinator manages, not scoped to one. Per-call session correlation is
// provided by loggingUIBridge, which wraps this bridge with the active
// call's sessionID.
func (b *coordinatorUIBridge) SessionID() string { return "" }

func (b *coordinatorUIBridge) Confirm(ctx context.Context, title, description string) (bool, error) {
	if b == nil || b.coordinator == nil || !b.coordinator.interactiveUI {
```
