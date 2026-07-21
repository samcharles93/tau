---
description: Source module pkg/taui/statusrow.go (65 lines).
resource: pkg/taui/statusrow.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: statusrow.go
type: Module
---

# Module statusrow.go

**Path**: `pkg/taui/statusrow.go`  
**Lines**: 65

## Snippet Preview

```
package taui

import "github.com/samcharles93/tau/pkg/taui/termkit"

// StatusRowState is the lifecycle state of a StatusRow.
type StatusRowState int

const (
	StatusRowRunning StatusRowState = iota
	StatusRowSuccess
	StatusRowFailed
	StatusRowNeutral
)

// StatusRow renders a single "<glyph> label - detail" line, reusing ToolRow's
// visual language (spinner / ✓ / ✗) without ToolRow's tool-call-specific
// "name (args)" formatting - this is what a plugin's generic StatusWidget
// renders through. Neutral renders a static bullet with no animation, for
// status that isn't an in-progress lifecycle.
type StatusRow struct {
	label  string
	detail string
	state  StatusRowState
	frame  int
}

// NewStatusRow creates a StatusRow in the given state.
func NewStatusRow(label, detail string, state StatusRowState) *StatusRow {
	return &StatusRow{label: label, detail: detail, state: state}
}
```
