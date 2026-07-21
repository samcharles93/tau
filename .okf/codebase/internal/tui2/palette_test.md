---
description: Source module internal/tui2/palette_test.go (313 lines).
resource: internal/tui2/palette_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: palette_test.go
type: Module
---

# Module palette_test.go

**Path**: `internal/tui2/palette_test.go`  
**Lines**: 313

## Snippet Preview

```
package tui2

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

func TestCtrlPOpensIndependentCommandPalette(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "draft prompt"
	m.inputCursor = len([]rune(m.input))

	m.dispatchKey(key('p', tea.ModCtrl))

	if m.palette == nil || m.palette.kind != paletteCommands {
		t.Fatal("expected command palette to be open")
	}
	if m.input != "draft prompt" {
		t.Fatalf("input = %q, want draft preserved", m.input)
	}
	if rows := m.paletteRows(); len(rows) == 0 {
		t.Fatal("expected command rows")
	}
}

func TestCommandPalettePrefillsQueryFromSlashInput(t *testing.T) {
```
