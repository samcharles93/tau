---
description: Source module internal/tui2/diff.go (129 lines).
resource: internal/tui2/diff.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: diff.go
type: Module
---

# Module diff.go

**Path**: `internal/tui2/diff.go`  
**Lines**: 129

## Snippet Preview

```
package tui2

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/pmezard/go-difflib/difflib"
	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/theme"
)

// diffViewerWidthFrac and diffViewerHeightFrac size the overlay relative to
// the terminal - big enough to actually read a diff, small enough to keep
// the surrounding chrome visible as context that something is layered on
// top of it.
const (
	diffViewerWidthFrac  = 0.9
	diffViewerHeightFrac = 0.85
)

// openDiffViewer builds and opens the "View diff" overlay for t, which must
// satisfy toolSupportsDiffView. Replaces any other open exclusive overlay,
// since activating a menu item always closes the menu it came from.
func (m *model) openDiffViewer(t toolState) tea.Cmd {
	m.closeOtherExclusiveOverlays(overlayDiff)

	details, ok := t.details.(tools.DiffDetails)
```
