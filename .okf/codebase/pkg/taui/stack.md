---
description: Source module pkg/taui/stack.go (78 lines).
resource: pkg/taui/stack.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: stack.go
type: Module
---

# Module stack.go

**Path**: `pkg/taui/stack.go`  
**Lines**: 78

## Snippet Preview

```
package taui

import "strings"

// StackDirection selects how Stack lays out its children.
type StackDirection int

const (
	StackVertical StackDirection = iota
	StackHorizontal
)

// Stack lays out child components vertically (delegating to Container, which
// only concatenates lines top-to-bottom) or horizontally, rendering each
// child independently and zipping their lines side by side with Gap spaces
// between columns, padding both mismatched widths and heights.
type Stack struct {
	Container
	Direction StackDirection
	Gap       int
}

// NewStack creates an empty Stack. Add children with AddChild.
func NewStack(direction StackDirection, gap int) *Stack {
	return &Stack{Direction: direction, Gap: gap}
}

// Render lays out children per Direction.
func (s *Stack) Render(width int) []string {
	if s.Direction == StackVertical {
```
