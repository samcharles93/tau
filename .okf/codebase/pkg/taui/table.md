---
description: Source module pkg/taui/table.go (93 lines).
resource: pkg/taui/table.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: table.go
type: Module
---

# Module table.go

**Path**: `pkg/taui/table.go`  
**Lines**: 93

## Snippet Preview

```
package taui

import "strings"

// tableColSep separates columns; its visible width (3) matches the "─┼─"
// separator used in the header rule, so columns line up.
const tableColSep = " │ "

// Table renders a header row, a separator rule, and body rows, with each
// column padded to its widest cell. If the natural width exceeds the render
// width, columns are shrunk proportionally and cells truncated to fit.
type Table struct {
	headers []string
	rows    [][]string
}

// NewTable creates a Table component.
func NewTable(headers []string, rows [][]string) *Table {
	return &Table{headers: headers, rows: rows}
}

// Invalidate is a no-op; Table holds no cached render.
func (t *Table) Invalidate() {}

// Render returns the header, separator, and body lines.
func (t *Table) Render(width int) []string {
	cols := len(t.headers)
	for _, r := range t.rows {
		if len(r) > cols {
			cols = len(r)
```
