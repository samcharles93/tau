---
description: Source module pkg/taui/paragraph.go (135 lines).
resource: pkg/taui/paragraph.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: paragraph.go
type: Module
---

# Module paragraph.go

**Path**: `pkg/taui/paragraph.go`  
**Lines**: 135

## Snippet Preview

```
package taui

import (
	"strings"
	"sync"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// Paragraph is a multi-line, word-wrapped text block - the component for
// streaming assistant output. Append tokens as they arrive; Render re-wraps to
// the current width, honoring any embedded newlines. Safe for concurrent use
// (an application goroutine appends while the render goroutine reads).
type Paragraph struct {
	mu   sync.Mutex
	text string
	fn   func(string) string
}

// NewParagraph creates a paragraph with initial text.
func NewParagraph(text string) *Paragraph { return &Paragraph{text: text} }

// SetText replaces the content.
func (p *Paragraph) SetText(s string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.text = s
}

// Append adds to the content (e.g. one streamed token).
```
