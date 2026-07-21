---
description: Source module internal/tui2/model_selection_test.go (534 lines).
resource: internal/tui2/model_selection_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: model_selection_test.go
type: Module
---

# Module model_selection_test.go

**Path**: `internal/tui2/model_selection_test.go`  
**Lines**: 534

## Snippet Preview

```
package tui2

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

func TestMouseClickFocusesAndExpandsTool(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.tools = []toolState{
		{id: "t1", name: "read", status: "done"},
		{id: "t2", name: "search", status: "done"},
	}

	geom := m.computeLayout()
	if len(geom.toolBoxes) != 2 {
		t.Fatalf("toolBoxes = %d, want 2", len(geom.toolBoxes))
	}

	// The expand toggle is a click action (press+release with no drag in
	// between) - see toggleToolBoxAtY - since press alone must instead arm
	// toolsSel in case the gesture turns into a drag-to-select.
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, Y: geom.toolBoxes[0].startY})
	m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, Y: geom.toolBoxes[0].startY})
```
