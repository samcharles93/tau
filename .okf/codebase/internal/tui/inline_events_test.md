---
description: Source module internal/tui/inline_events_test.go (336 lines).
resource: internal/tui/inline_events_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: inline_events_test.go
type: Module
---

# Module inline_events_test.go

**Path**: `internal/tui/inline_events_test.go`  
**Lines**: 336

## Snippet Preview

```
package tui

import (
	"testing"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/tui/notify"
	"github.com/samcharles93/tau/pkg/taui"
)

// runHandleEvent runs handleEvent in its own goroutine and fails the test if
// it panics or doesn't return within the timeout. handleEvent locks c.mu on
// entry and unlocks/re-locks it around UI calls in several branches; a
// mismatched Lock/Unlock pair either panics ("unlock of unlocked mutex") or
// deadlocks the event loop, so both failure modes need to be caught.
func runHandleEvent(t *testing.T, c *inlineChat, ev tauchat.ChatEvent) {
	t.Helper()

	panicked := make(chan any, 1)
	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				panicked <- r
			}
			close(done)
		}()
		c.handleEvent(ev)
	}()
```
