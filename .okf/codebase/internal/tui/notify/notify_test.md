---
description: Source module internal/tui/notify/notify_test.go (85 lines).
resource: internal/tui/notify/notify_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: notify_test.go
type: Module
---

# Module notify_test.go

**Path**: `internal/tui/notify/notify_test.go`  
**Lines**: 85

## Snippet Preview

```
package notify

import (
	"testing"
	"time"
)

// TestQueue_ZeroDurationPersists guards against a regression where
// Duration: 0 (documented as "never expires") was computed as
// expiresAt = time.Now(), so prune() discarded it on the very next check -
// a "persistent" notification vanished almost instantly instead of staying
// current until Dismiss.
func TestQueue_ZeroDurationPersists(t *testing.T) {
	q := NewQueue()
	q.Push(Notification{Message: "persistent error", Level: LevelError, Duration: 0})

	time.Sleep(5 * time.Millisecond)

	n := q.Current()
	if n == nil {
		t.Fatal("expected zero-duration notification to still be current")
	}
	if n.Message != "persistent error" {
		t.Fatalf("unexpected message: %q", n.Message)
	}
}

// TestQueue_PositiveDurationExpires confirms a normal, positive-duration
// notification still expires once its window elapses.
func TestQueue_PositiveDurationExpires(t *testing.T) {
```
