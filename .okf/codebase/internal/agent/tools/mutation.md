---
description: Source module internal/agent/tools/mutation.go (87 lines).
resource: internal/agent/tools/mutation.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: mutation.go
type: Module
---

# Module mutation.go

**Path**: `internal/agent/tools/mutation.go`  
**Lines**: 87

## Snippet Preview

```
package tools

import (
	"sync"
)

// MutationQueue serializes write operations to the same file path,
// preventing concurrent edits from clobbering each other during
// parallel tool execution.
//
// A sync.RWMutex coordinates between shell commands and file-mutation
// tools. File mutations (write, edit) take a read lock so they
// can run concurrently with each other. Shell commands take the write
// lock, blocking all file mutations for the duration of the command.
type MutationQueue struct {
	mu    sync.Mutex
	locks map[string]*mutexEntry

	globalMu sync.RWMutex
}

// mutexEntry tracks a per-file mutex and its active holder count.
type mutexEntry struct {
	mu      sync.Mutex
	holders int
}

// NewMutationQueue creates a new per-file mutation queue.
func NewMutationQueue() *MutationQueue {
	return &MutationQueue{
```
