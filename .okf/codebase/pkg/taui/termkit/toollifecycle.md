---
description: Source module pkg/taui/termkit/toollifecycle.go (143 lines).
resource: pkg/taui/termkit/toollifecycle.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: toollifecycle.go
type: Module
---

# Module toollifecycle.go

**Path**: `pkg/taui/termkit/toollifecycle.go`  
**Lines**: 143

## Snippet Preview

```
package termkit

import (
	"fmt"
	"io"
	"os"
	"time"
)

// ToolLifecycle renders the three-state inline tool-call lifecycle:
// an animated RUNNING line that is overwritten in place, resolving to either
// SUCCESS or FAILURE. It writes to an io.Writer (e.g. a go-tui StreamWriter
// or os.Stdout).
//
// Two modes are available:
//
//	// Determinate - spinner + progress bar (for tools with a known step count):
//	tl := NewToolLifecycle("go build", "./...", w)
//
//	// Indeterminate - spinner only (for tools with unknown duration):
//	tl := NewIndeterminateToolLifecycle("fetch", "https://api.example.com", w)
//
//	tl.Start()
//	for !done {
//	    tl.Tick()
//	    time.Sleep(40 * time.Millisecond)
//	}
//	tl.Resolve(true, "done in 1.2s")
type ToolLifecycle struct {
	toolName      string
```
