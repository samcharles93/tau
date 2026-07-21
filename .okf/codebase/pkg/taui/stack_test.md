---
description: Source module pkg/taui/stack_test.go (55 lines).
resource: pkg/taui/stack_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: stack_test.go
type: Module
---

# Module stack_test.go

**Path**: `pkg/taui/stack_test.go`  
**Lines**: 55

## Snippet Preview

```
package taui

import "testing"

func TestStackVerticalDelegatesToContainer(t *testing.T) {
	s := NewStack(StackVertical, 0)
	s.AddChild(NewText("one"))
	s.AddChild(NewText("two"))
	lines := s.Render(80)
	want := []string{"one", "two"}
	if len(lines) != len(want) {
		t.Fatalf("got %d lines, want %d: %v", len(lines), len(want), lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestStackHorizontalZipsColumns(t *testing.T) {
	s := NewStack(StackHorizontal, 1)
	s.AddChild(NewText("left"))
	s.AddChild(NewText("right"))
	lines := s.Render(40)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1: %v", len(lines), lines)
	}
	if lines[0] != "left right" {
		t.Errorf("got %q, want %q", lines[0], "left right")
```
