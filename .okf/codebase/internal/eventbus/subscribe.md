---
description: Source module internal/eventbus/subscribe.go (390 lines).
resource: internal/eventbus/subscribe.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: subscribe.go
type: Module
---

# Module subscribe.go

**Path**: `internal/eventbus/subscribe.go`  
**Lines**: 390

## Snippet Preview

```
package eventbus

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"
)

// --- subscriber interface ---

// subscriber is a uniformly typed wrapper around the typed subscriber
// implementations, so the bus can dispatch without knowing about T.
type subscriber interface {
	subscribeType() reflect.Type
	// dispatch delivers the head value in vals to the subscriber while
	// also handling stop and incoming queue write events.
	dispatch(
		ctx context.Context,
		vals *queue[DeliveredEvent],
		acceptCh func() chan DeliveredEvent,
		snapshot chan chan []DeliveredEvent,
	) bool
	Close()
}

// --- subscribeState ---

// subscribeState handles dispatching events received from a Bus to a
```
