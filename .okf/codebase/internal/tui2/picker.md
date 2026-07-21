---
description: Source module internal/tui2/picker.go (220 lines).
resource: internal/tui2/picker.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: picker.go
type: Module
---

# Module picker.go

**Path**: `internal/tui2/picker.go`  
**Lines**: 220

## Snippet Preview

```
package tui2

import (
	"fmt"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

type pickerAction int

const (
	pickerActionNone pickerAction = iota
	pickerActionClose
	pickerActionSelect
)

// listPicker is a reusable searchable-list component. It owns only the UI
// mechanics that every picker shares: query editing, selection, scrolling,
// and rendering. Callers remain responsible for supplying rows and applying
// the selected value.
type listPicker struct {
	title    string
	query    string
	cursor   int
	selected int
}

func newListPicker(title string) listPicker {
```
