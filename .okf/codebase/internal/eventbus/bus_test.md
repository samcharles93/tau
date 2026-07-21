---
description: Source module internal/eventbus/bus_test.go (130 lines).
resource: internal/eventbus/bus_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: bus_test.go
type: Module
---

# Module bus_test.go

**Path**: `internal/eventbus/bus_test.go`  
**Lines**: 130

## Snippet Preview

```
package eventbus

import (
	"testing"
	"time"
)

type testEvent struct{ Msg string }

func TestBusPublishAndSubscribe(t *testing.T) {
	bus := New()
	defer bus.Close()

	pubClient := bus.Client("publisher")
	subClient := bus.Client("subscriber")

	pub := Publish[testEvent](pubClient)
	sub := Subscribe[testEvent](subClient)

	pub.Publish(testEvent{Msg: "hello"})

	select {
	case ev := <-sub.Events():
		if ev.Msg != "hello" {
			t.Errorf("got %q, want hello", ev.Msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}
```
