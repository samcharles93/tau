---
description: Source module pkg/taui/divider_test.go (195 lines).
resource: pkg/taui/divider_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: divider_test.go
type: Module
---

# Module divider_test.go

**Path**: `pkg/taui/divider_test.go`  
**Lines**: 195

## Snippet Preview

```
package taui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

func TestDividerPlainFillsWidth(t *testing.T) {
	d := NewDivider("")
	lines := d.Render(10)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if got := VisibleWidth(lines[0]); got != 10 {
		t.Errorf("width = %d, want 10: %q", got, lines[0])
	}
}

func TestDividerCentersLabel(t *testing.T) {
	d := NewDivider("Results")
	lines := d.Render(20)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if !strings.Contains(lines[0], "Results") {
		t.Errorf("expected label in divider line: %q", lines[0])
	}
```
