---
description: Source module internal/agent/tools/spawn_admission.go (242 lines).
resource: internal/agent/tools/spawn_admission.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: spawn_admission.go
type: Module
---

# Module spawn_admission.go

**Path**: `internal/agent/tools/spawn_admission.go`  
**Lines**: 242

## Snippet Preview

```
package tools

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/samcharles93/tau/internal/config"
)

// spawnAdmission enforces the concurrency and resource ceilings from
// docs/specs/agents/02-spawning-and-lifecycle.md (Concurrency and resource
// ceilings): a per-parent-instance active-child limit, a process-wide
// active-child limit, and a per-parent FIFO spawn queue. One instance is
// shared process-wide across every agent tool call in this OS process,
// since max_total_children bounds the whole process, not any single call.
type spawnAdmission struct {
	mu          sync.Mutex
	totalActive int
	parents     map[string]int // parentInstanceID -> active count
	queue       []*spawnWaiter // global FIFO across all parents
}

type spawnWaiter struct {
	parentInstanceID string
	ready            chan struct{}
	removed          bool
}

```
