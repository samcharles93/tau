---
description: Source module pkg/taui/renderer_test.go (294 lines).
resource: pkg/taui/renderer_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: renderer_test.go
type: Module
---

# Module renderer_test.go

**Path**: `pkg/taui/renderer_test.go`  
**Lines**: 294

## Snippet Preview

```
package taui

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

// doRender acquires the render lock and draws one frame. Used by tests to
// force a render without managing the lock; production render timer calls
// renderLocked directly under the lock.
func (t *TUI) doRender() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.renderLocked()
}

// absPosRe matches absolute cursor positioning: CSI ... H (home / CUP).
// An inline renderer must never emit these - they anchor the frame to the top
// of the viewport instead of where it was first drawn.
var absPosRe = regexp.MustCompile("\x1b\\[[0-9;]*H")

// fakeTerm is a Terminal that records everything written to it and reports a
// fixed size, so renders are fully deterministic.
type fakeTerm struct {
	cols, rows int
	buf        strings.Builder
}

```
