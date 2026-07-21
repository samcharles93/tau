---
description: Source module internal/tui2/selection.go (482 lines).
resource: internal/tui2/selection.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: selection.go
type: Module
---

# Module selection.go

**Path**: `internal/tui2/selection.go`  
**Lines**: 482

## Snippet Preview

```
package tui2

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// dragRegion identifies which UI region a mouse press/drag/release is
// operating on, so a single input event stream can drive three independent
// selectionStates (viewport, input box, status bar) without them
// interfering.
type dragRegion int

const (
	dragNone dragRegion = iota
	dragViewport
	dragInput
	dragStatus
	dragTools
)

// selectionState is a press→drag→release text-selection gesture over some
// region's content, addressed by a single ordered integer position - a
// line index, a rune index, a column, whatever that region's own
// coordinate space is. Any UI region gets full drag-to-select behavior (via
// finalizeSelection) just by driving one of these plus a small
```
