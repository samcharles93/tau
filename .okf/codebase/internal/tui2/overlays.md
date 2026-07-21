---
description: Source module internal/tui2/overlays.go (507 lines).
resource: internal/tui2/overlays.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: overlays.go
type: Module
---

# Module overlays.go

**Path**: `internal/tui2/overlays.go`  
**Lines**: 507

## Snippet Preview

```
package tui2

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// contextMenuTarget identifies what kind of element a context menu was
// opened against - tool calls have two distinct on-screen forms (see
// contextMenuTargetTool vs contextMenuTargetToolRow), because a live,
// uncommitted tool box and a committed group's unfolded per-tool row are
// resolved through completely different hit-testing paths.
type contextMenuTarget int

const (
	contextMenuTargetNone    contextMenuTarget = iota
	contextMenuTargetTool                      // a live geom.toolBoxes entry, or a whole (folded or unfolded) committed group
	contextMenuTargetToolRow                   // one row inside an unfolded committed group
	contextMenuTargetMessage
)

// contextMenuAction identifies what a menu item does when activated.
type contextMenuAction int
```
