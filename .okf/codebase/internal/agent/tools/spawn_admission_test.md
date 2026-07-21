---
description: Source module internal/agent/tools/spawn_admission_test.go (320 lines).
resource: internal/agent/tools/spawn_admission_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: spawn_admission_test.go
type: Module
---

# Module spawn_admission_test.go

**Path**: `internal/agent/tools/spawn_admission_test.go`  
**Lines**: 320

## Snippet Preview

```
package tools

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/config"
)

// --- G5: concurrency and resource ceilings ---
// Saturation test requirements per docs/specs/agents/
// 02-spawning-and-lifecycle.md (Concurrency and resource ceilings). Each
// test uses its own *spawnAdmission instance rather than the process-wide
// globalSpawnAdmission, so tests don't interfere with each other.

func agentsCfgFor(maxActive, maxTotal, maxQueued int) config.AgentsConfig {
	return config.AgentsConfig{
		MaxActiveChildren: maxActive,
		MaxTotalChildren:  maxTotal,
		MaxQueuedSpawns:   maxQueued,
	}
}

// TestSpawnAdmission_FifthQueuesWhenActiveAtMax: "5 agent calls when
// max_active=4 -> 4 start immediately, 1 queued".
func TestSpawnAdmission_FifthQueuesWhenActiveAtMax(t *testing.T) {
	s := newSpawnAdmission()
	cfg := agentsCfgFor(4, 16, 8)
```
