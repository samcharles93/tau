---
description: Source module internal/tui2/input.go (731 lines).
resource: internal/tui2/input.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: input.go
type: Module
---

# Module input.go

**Path**: `internal/tui2/input.go`  
**Lines**: 731

## Snippet Preview

```
package tui2

import (
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

// clearInput resets the input buffer and cursor together - every reset site
// must clear both or the cursor can end up pointing past the end of a
// shorter (or empty) buffer.
func (m *model) clearInput() {
	m.input = ""
	m.inputCursor = 0
	m.inputModeCommand = ""
	m.inputSel.clear()
	m.compDismissed = false
	m.compDismissedToken = ""
}

// clearScreen wipes the visible scrollback (Ctrl+Shift+L) without touching the
// underlying chat session - unlike /clear, which sends a
// ResetChatSessionCommand and actually starts a new session, this only
// clears what's rendered locally. The next ChatSessionSnapshotEvent (e.g.
// from /session, /resume) still rebuilds renderedLines from the real
```
