---
description: Source module internal/tui2/help.go (495 lines).
resource: internal/tui2/help.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: help.go
type: Module
---

# Module help.go

**Path**: `internal/tui2/help.go`  
**Lines**: 495

## Snippet Preview

```
package tui2

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/samcharles93/tau/internal/theme"
)

// helpRow is one keybinding entry: the key combo and what it does.
type helpRow struct {
	key  string
	desc string
}

// helpSection groups related keybindings under a titled divider.
type helpSection struct {
	title string
	rows  []helpRow
}

// helpSections is the single source of truth for /help's keybinding
// listing - slash commands are deliberately not repeated here, since they
// already live in the "/" completions dropdown (internal/tui2/completions.go).
// Indices into this slice are what helpRowKey.section refers to.
var helpSections = []helpSection{
	{
		title: "Input & Editing",
```
