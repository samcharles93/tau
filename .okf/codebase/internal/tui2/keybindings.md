---
description: Source module internal/tui2/keybindings.go (398 lines).
resource: internal/tui2/keybindings.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: keybindings.go
type: Module
---

# Module keybindings.go

**Path**: `internal/tui2/keybindings.go`  
**Lines**: 398

## Snippet Preview

```
package tui2

import (
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

// handleKey dispatches a keypress and then re-syncs the completions
// selection against whatever the keystroke just did to m.input. The sync
// can't happen only inside handleCompletionKey's own pre-dispatch check -
// that check runs BEFORE a character insertion/deletion below it in the same
// keystroke, so it always compares against the token as of the START of this
// call. Without the post-dispatch sync, a query-narrowing keystroke leaves
// compSelected pointing at a stale index for one extra render frame instead
// of resetting immediately.
func (m *model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	cmd := m.dispatchKey(msg)
	m.syncCompletionSelection()
	if paletteCmd, opened := m.maybeOpenInputPalette(); opened {
		return tea.Batch(cmd, paletteCmd)
	}
	return tea.Batch(cmd, m.maybePrefetchSessions())
}

// syncCompletionSelection resets compSelected to the top match whenever the
```
