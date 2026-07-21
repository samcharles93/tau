---
description: Source module internal/eventbus/queue.go (81 lines).
resource: internal/eventbus/queue.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: queue.go
type: Module
---

# Module queue.go

**Path**: `internal/eventbus/queue.go`  
**Lines**: 81

## Snippet Preview

```
// Package eventbus provides an in-process, type-safe event bus connecting
// publishers of typed events with subscribers interested in those events.
//
// The core design is adapted from Tailscale's util/eventbus package
// (BSD-3-Clause, Tailscale Inc & contributors).
package eventbus

import "slices"

// queue is an ordered queue of values up to capacity, if capacity is
// non-zero. Otherwise it is unbounded.
type queue[T any] struct {
	vals     []T
	start    int
	capacity int // zero means unbounded
}

// canAppend reports whether a value can be appended to q.vals without
// shifting values around.
func (q *queue[T]) canAppend() bool {
	return q.capacity == 0 || cap(q.vals) < q.capacity || len(q.vals) < cap(q.vals)
}

// Full reports whether the queue is at capacity and cannot accept more
// values without a shift.
func (q *queue[T]) Full() bool {
	return q.start == 0 && !q.canAppend()
}

// Empty reports whether the queue contains no values.
```
