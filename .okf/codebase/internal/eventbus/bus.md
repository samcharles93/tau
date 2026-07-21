---
description: Source module internal/eventbus/bus.go (289 lines).
resource: internal/eventbus/bus.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: bus.go
type: Module
---

# Module bus.go

**Path**: `internal/eventbus/bus.go`  
**Lines**: 289

## Snippet Preview

```
package eventbus

import (
	"context"
	"reflect"
	"slices"
	"sync"
	"time"
)

// PublishedEvent wraps an event alongside its source client and declared
// publish type. The Type field carries the publisher's declared type parameter
// rather than the concrete value type, so that interface-typed publishers
// (e.g. Publisher[ChatEvent]) route correctly to subscribers of the same
// interface type.
type PublishedEvent struct {
	Event any
	Type  reflect.Type
	From  *Client
}

// DeliveredEvent wraps an event alongside its source, destination, and
// declared publish type. Type carries the publisher's type parameter so
// that interface-typed routing works correctly.
type DeliveredEvent struct {
	Event any
	Type  reflect.Type
	From  *Client
	To    *Client
}
```
