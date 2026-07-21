---
description: Source module pkg/taui/list.go (58 lines).
resource: pkg/taui/list.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: list.go
type: Module
---

# Module list.go

**Path**: `pkg/taui/list.go`  
**Lines**: 58

## Snippet Preview

```
package taui

import (
	"strconv"
	"strings"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// List renders bulleted or numbered items, word-wrapping continuation lines
// with a hanging indent so wrapped text aligns under the item text.
type List struct {
	items   []string
	ordered bool
	fn      func(string) string
}

// NewList creates a List component.
func NewList(items []string, ordered bool) *List {
	return &List{items: items, ordered: ordered}
}

// SetStyle sets a colour callback applied to each rendered line.
func (l *List) SetStyle(fn func(string) string) { l.fn = fn }

// Invalidate is a no-op; List holds no cached render.
func (l *List) Invalidate() {}

// Render returns the wrapped, marker-prefixed lines for every item.
func (l *List) Render(width int) []string {
```
