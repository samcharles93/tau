---
description: Source module pkg/taui/list_test.go (57 lines).
resource: pkg/taui/list_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: list_test.go
type: Module
---

# Module list_test.go

**Path**: `pkg/taui/list_test.go`  
**Lines**: 57

## Snippet Preview

```
package taui

import (
	"strings"
	"testing"
)

func TestListBulleted(t *testing.T) {
	l := NewList([]string{"first", "second"}, false)
	lines := l.Render(80)
	want := []string{"• first", "• second"}
	if len(lines) != len(want) {
		t.Fatalf("got %d lines, want %d: %v", len(lines), len(want), lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestListOrdered(t *testing.T) {
	l := NewList([]string{"first", "second"}, true)
	lines := l.Render(80)
	want := []string{"1. first", "2. second"}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
```
