---
description: Source module internal/agent/tools/mutation_test.go (68 lines).
resource: internal/agent/tools/mutation_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: mutation_test.go
type: Module
---

# Module mutation_test.go

**Path**: `internal/agent/tools/mutation_test.go`  
**Lines**: 68

## Snippet Preview

```
package tools_test

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/samcharles93/tau/internal/agent/tools"
)

func TestMutationQueue_Serializes(t *testing.T) {
	q := tools.NewMutationQueue()
	path := "/tmp/test.txt"

	var counter atomic.Int64
	var maxConcurrent atomic.Int64
	var wg sync.WaitGroup

	for i := range 50 {
		_ = i
		wg.Go(func() {
			release := q.Acquire(path)
			defer release()

			cur := counter.Add(1)
			// Track max concurrency for this path.
			for {
				old := maxConcurrent.Load()
				if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
					break
```
