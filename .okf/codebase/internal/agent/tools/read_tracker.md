---
description: Source module internal/agent/tools/read_tracker.go (54 lines).
resource: internal/agent/tools/read_tracker.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: read_tracker.go
type: Module
---

# Module read_tracker.go

**Path**: `internal/agent/tools/read_tracker.go`  
**Lines**: 54

## Snippet Preview

```
package tools

import (
	"fmt"
	"path/filepath"
	"sync"
)

// ReadTracker records which files the model has read so that mutation
// tools (write, edit) can enforce a read-before-write safety
// check.
type ReadTracker struct {
	mu    sync.Mutex
	reads map[string]bool // absolute paths that have been read
}

// NewReadTracker creates a new ReadTracker.
func NewReadTracker() *ReadTracker {
	return &ReadTracker{
		reads: make(map[string]bool),
	}
}

// MarkRead records that a file at the given path has been read by the
// model. The path is normalised to absolute form before recording.
func (rt *ReadTracker) MarkRead(cwd, path string) {
	abs := resolvePath(cwd, path)
	rt.mu.Lock()
	rt.reads[abs] = true
	rt.mu.Unlock()
```
