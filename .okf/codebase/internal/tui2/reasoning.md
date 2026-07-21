---
description: Source module internal/tui2/reasoning.go (206 lines).
resource: internal/tui2/reasoning.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: reasoning.go
type: Module
---

# Module reasoning.go

**Path**: `internal/tui2/reasoning.go`  
**Lines**: 206

## Snippet Preview

```
package tui2

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// committedReasoningBlock is a completed reasoning block already committed
// to permanent scrollback that can still be collapsed/expanded afterward -
// mirroring committedToolGroup's same "stay interactive after it scrolls
// into history" shape (see spliceCommittedReasoning). Only a finished turn's
// reasoning gets one of these; the in-progress turn's reasoning is rendered
// fresh every frame straight from m.reasoning (see viewportLinesForView) and
// is never a candidate for collapse - "streaming reasoning stays visible
// while active" falls out of that split for free, no separate flag needed.
type committedReasoningBlock struct {
	key       string // stable across an applySnapshot rebuild - see committedReasoningKey
	text      string // raw reasoning text; unaffected by collapse (presentation-only)
	collapsed bool

	// lineIdx/lineCount mirror committedToolGroup's own fields - see
	// spliceCommittedReasoning.
	lineIdx   int
	lineCount int
}

// committedReasoningKey derives a stable key for a completed reasoning
// block so its collapse state survives an applySnapshot rebuild instead of
```
