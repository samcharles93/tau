---
description: Source module internal/agent/tools/agent_session_id_test.go (58 lines).
resource: internal/agent/tools/agent_session_id_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: agent_session_id_test.go
type: Module
---

# Module agent_session_id_test.go

**Path**: `internal/agent/tools/agent_session_id_test.go`  
**Lines**: 58

## Snippet Preview

```
package tools

import (
	"sync"
	"testing"
)

// TestGenerateSessionID_NoCollisionsUnderConcurrency reproduces a real
// collision risk: generateSessionID used to be "child-" + time.Now().UnixNano(),
// pure wall-clock time with no randomness. instantiateChild calls it once per
// spawned child agent, and the coordinator explicitly supports spawning
// children concurrently (parallel tool calls), so two children minted on
// different goroutines within the same clock tick got the identical session
// ID. That ID becomes the child's persisted session row - sessions.Manager.Save
// upserts by ID and deletes+reinserts that ID's messages, so a collision
// silently merges one child's transcript into the other's instead of
// erroring. Runs many concurrent mints and asserts every ID is unique.
func TestGenerateSessionID_NoCollisionsUnderConcurrency(t *testing.T) {
	const n = 5000
	ids := make([]string, n)
	errs := make([]error, n)

	// A start barrier is essential to reproduce this: goroutines released one
	// at a time (bare `go func(){...}()` in a loop) pick up enough scheduling
	// jitter between spawn and body that the clock has almost always ticked
	// forward by the time each one reads it. Releasing all n at once via a
	// closed channel is what actually lines multiple goroutines up on the
	// same clock tick, matching how the coordinator fires off several
	// parallel "agent" tool calls at once.
	var wg sync.WaitGroup
```
