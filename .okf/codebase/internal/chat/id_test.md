---
description: Source module internal/chat/id_test.go (86 lines).
resource: internal/chat/id_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: id_test.go
type: Module
---

# Module id_test.go

**Path**: `internal/chat/id_test.go`  
**Lines**: 86

## Snippet Preview

```
package chat

import (
	"strings"
	"sync"
	"testing"
)

func TestNewID_ReturnsDistinctUUIDv7(t *testing.T) {
	a, err := NewID()
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	b, err := NewID()
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	if a == b {
		t.Fatalf("two calls to NewID returned the same id %q", a)
	}
	if !strings.Contains(a, "-") {
		t.Fatalf("expected a UUID-shaped id, got %q", a)
	}
}

// TestNewID_NoCollisionsUnderConcurrency guards the property that motivated
// consolidating every ID helper onto NewID: internal/agent/tools once minted
// child agent session IDs from a bare time.Now().UnixNano(), which collided
// when several children were spawned concurrently (see generateSessionID's
// history). A start barrier releases all goroutines at once so they call
```
