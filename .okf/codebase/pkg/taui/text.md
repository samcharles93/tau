---
description: Source module pkg/taui/text.go (55 lines).
resource: pkg/taui/text.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: text.go
type: Module
---

# Module text.go

**Path**: `pkg/taui/text.go`  
**Lines**: 55

## Snippet Preview

```
package taui

import "github.com/samcharles93/tau/pkg/taui/termkit"

// Text is the simplest component - it renders a single string with optional
// foreground and background colour callbacks. Ported from Pi's components/text.ts.
type Text struct {
	text string
	fgFn func(string) string
	bgFn func(string) string
}

// NewText creates a Text component.
func NewText(text string) *Text {
	return &Text{text: text}
}

// NewStyledText creates a Text component with fg/bg colour callbacks.
func NewStyledText(text string, fgFn, bgFn func(string) string) *Text {
	return &Text{text: text, fgFn: fgFn, bgFn: bgFn}
}

// SetText updates the text content.
func (t *Text) SetText(text string) {
	t.text = text
}

// Text returns the current text.
func (t *Text) Text() string { return t.text }

```
