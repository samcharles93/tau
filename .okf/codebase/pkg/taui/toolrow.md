---
description: Source module pkg/taui/toolrow.go (210 lines).
resource: pkg/taui/toolrow.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: toolrow.go
type: Module
---

# Module toolrow.go

**Path**: `pkg/taui/toolrow.go`  
**Lines**: 210

## Snippet Preview

```
package taui

import (
	"fmt"
	"sync"
	"time"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// ToolState is the lifecycle state of a ToolRow.
type ToolState int

const (
	ToolRunning ToolState = iota
	ToolSuccess
	ToolFailed
)

// ToolStyle selects how a ToolRow renders. tau is expected to make this
// configurable (see NOTES.md).
type ToolStyle int

const (
	// ToolStyleCombined renders fg-only - a spinner / ✓ / ✗ glyph plus grey
	// text, with no embedded reset - so the row composes inside a coloured Box
	// (the look from examples/combined). This is the default.
	ToolStyleCombined ToolStyle = iota
	// ToolStyleBadge renders bg-chip SUCCESS / FAILED badges on the default
	// background (the look from examples/tooldemo). Not safe inside a coloured
```
