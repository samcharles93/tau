---
description: Source module internal/chat/events_test.go (23 lines).
resource: internal/chat/events_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: events_test.go
type: Module
---

# Module events_test.go

**Path**: `internal/chat/events_test.go`  
**Lines**: 23

## Snippet Preview

```
package chat

import (
	"testing"
	"time"
)

func TestChatToolOutputEvent(t *testing.T) {
	ev := ChatToolOutputEvent{
		SessionID:  "s1",
		RequestID:  "r1",
		CallID:     "c1",
		Chunk:      "hello world",
		ReceivedAt: time.Now(),
	}

	// Verify it implements ChatEvent
	var _ ChatEvent = ev

	if ev.Chunk != "hello world" {
		t.Errorf("expected chunk 'hello world', got %q", ev.Chunk)
	}
}
```
