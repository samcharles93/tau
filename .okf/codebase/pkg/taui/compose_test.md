---
description: Source module pkg/taui/compose_test.go (61 lines).
resource: pkg/taui/compose_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: compose_test.go
type: Module
---

# Module compose_test.go

**Path**: `pkg/taui/compose_test.go`  
**Lines**: 61

## Snippet Preview

```
package taui

import (
	"strings"
	"testing"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// TestToolRowComposesInColouredBox guards the "combined format" requirement:
// a ToolRow (default ToolStyleCombined) must render fg-only so it sits on a
// coloured Box background without terminating it. A stray full reset (\x1b[0m)
// inside the row would cut the box background mid-line.
func TestToolRowComposesInColouredBox(t *testing.T) {
	termkit.ForceColor()
	defer termkit.DisableColor()

	row := NewToolRow("go", "build ./...")
	row.Succeed("done in 1.1s")

	box := NewBox().Padding(1, 1).ExpandW().
		Bg(func(s string) string { return termkit.FgBgOnly(s, termkit.ColorObsidian, termkit.ColorAmber) }).
		Build()
	box.AddChild(row)

	var rowLine string
	for _, ln := range box.Render(60) {
		if strings.Contains(ln, "build ./...") {
			rowLine = ln
		}
```
