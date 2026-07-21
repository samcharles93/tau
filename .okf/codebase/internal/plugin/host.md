---
description: Source module internal/plugin/host.go (313 lines).
resource: internal/plugin/host.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: host.go
type: Module
---

# Module host.go

**Path**: `internal/plugin/host.go`  
**Lines**: 313

## Snippet Preview

```
package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/samcharles93/tau/pkg/plugin/api"
)

// Notifier pushes a user-visible notification to the host UI (TUI + web).
type Notifier func(level, message string)

// InteractiveHandler lets plugins prompt the user for confirmation or text input.
// It is set by the coordinator via SetInteractiveHandler.
type InteractiveHandler interface {
	Confirm(ctx context.Context, title, description string) (bool, error)
	Input(ctx context.Context, title, placeholder string) (string, error)
}

// ViewRenderer lets plugins open, update, and close persistent panels in the
// host UI via the HostService RenderView/CloseView RPCs. It is set by the
// coordinator via Manager.SetViewRenderer.
type ViewRenderer interface {
	RenderView(ctx context.Context, pluginName string, view *api.View) error
```
