---
description: Source module internal/tui2/render.go (355 lines).
resource: internal/tui2/render.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: render.go
type: Module
---

# Module render.go

**Path**: `internal/tui2/render.go`  
**Lines**: 355

## Snippet Preview

```
package tui2

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

// messageLineRange records the [startLine, endLine) span (half-open,
// indices into m.renderedLines at the time of recording) that one
// ChatMessage's rendered lines occupy. Unlike toolBoxGeometry, this indexes
// m.renderedLines directly rather than final screen rows - logicalLineAtRow
// already maps a screen row to a renderedLines index, so no separate
// box-relative-to-absolute translation is needed in computeLayout.
type messageLineRange struct {
	id        string
	content   string // raw (unstyled) message content, for Copy - renderedLines is lipgloss-styled, same reason lastAssistantText is kept separately
	startLine int
	endLine   int
}

// streamCursor is the block cursor shown immediately after the actively
// streaming assistant response. It is presentation-only and never written
// into persisted content or copied response text.
const streamCursor = "▋"

```
