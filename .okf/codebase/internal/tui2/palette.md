---
description: Source module internal/tui2/palette.go (241 lines).
resource: internal/tui2/palette.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: palette.go
type: Module
---

# Module palette.go

**Path**: `internal/tui2/palette.go`  
**Lines**: 241

## Snippet Preview

```
package tui2

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// paletteWidthFrac/paletteMinWidth/paletteMaxWidth size the shared Ctrl+P
// command palette and Ctrl+L model picker. The Ctrl+O session tree reuses the
// width helper, but remains a separate component with its own state and
// rendering.
const (
	paletteWidthFrac = 0.6
	paletteMinWidth  = 40
	paletteMaxWidth  = 70
)

type paletteKind int

const (
	paletteCommands paletteKind = iota
	paletteModels
	paletteProviders
)

// paletteState owns the floating picker's input and selection. Keeping this
// separate from model.input is what lets the palette present a real search
```
