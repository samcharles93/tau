---
description: Source module pkg/taui/table_test.go (56 lines).
resource: pkg/taui/table_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: table_test.go
type: Module
---

# Module table_test.go

**Path**: `pkg/taui/table_test.go`  
**Lines**: 56

## Snippet Preview

```
package taui

import (
	"strings"
	"testing"
)

func TestTableRendersHeaderSeparatorAndRows(t *testing.T) {
	tbl := NewTable([]string{"name", "status"}, [][]string{
		{"go", "ok"},
		{"lint", "failed"},
	})
	lines := tbl.Render(80)
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4 (header, rule, 2 rows): %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "name") || !strings.Contains(lines[0], "status") {
		t.Errorf("header line missing headers: %q", lines[0])
	}
	if !strings.Contains(lines[1], "─") {
		t.Errorf("second line should be the separator rule: %q", lines[1])
	}
	if !strings.Contains(lines[2], "go") || !strings.Contains(lines[3], "lint") {
		t.Errorf("body rows missing expected content: %v", lines[2:])
	}
}

func TestTableColumnsAlignToWidestCell(t *testing.T) {
	tbl := NewTable([]string{"k"}, [][]string{{"a-very-long-value"}})
	lines := tbl.Render(80)
```
