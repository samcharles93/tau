---
description: Source module pkg/taui/taillog.go (102 lines).
resource: pkg/taui/taillog.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: taillog.go
type: Module
---

# Module taillog.go

**Path**: `pkg/taui/taillog.go`  
**Lines**: 102

## Snippet Preview

```
package taui

import (
	"strings"
	"sync"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// TailLog is a bounded, live-updating view of streamed text: it keeps only
// the last MaxLines lines, dropping older ones as new output arrives. Use it
// for tool stdout/stderr while a tool is running - unlike printing every
// chunk straight to scrollback, the tail stays a fixed size and is meant to
// be discarded (RemoveChild it from its parent) once the producer finishes,
// rather than committed to permanent output.
//
// TailLog is safe for concurrent use: a producer goroutine calls Append while
// the render goroutine calls Render, mirroring ToolRow's locking.
type TailLog struct {
	mu       sync.Mutex
	lines    []string // bounded ring, oldest first
	partial  string   // buffered line with no trailing newline yet
	maxLines int
	styleFn  FgFn
}

// NewTailLog creates a tail log that keeps at most maxLines lines. styleFn,
// if non-nil, is applied to each rendered line (e.g. a dim/grey color).
func NewTailLog(maxLines int, styleFn FgFn) *TailLog {
	if maxLines <= 0 {
```
