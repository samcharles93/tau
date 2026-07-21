---
description: Source module internal/eventbus/publish.go (176 lines).
resource: internal/eventbus/publish.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: publish.go
type: Module
---

# Module publish.go

**Path**: `internal/eventbus/publish.go`  
**Lines**: 176

## Snippet Preview

```
package eventbus

import (
	"log/slog"
	"reflect"
	"sync"
)

// --- Client ---

// A Client can publish and subscribe to events on its attached bus.
// Use [Publish] to publish events and [Subscribe] to receive them.
//
// Subscribers that share the same client receive events one at a time,
// in publication order.
type Client struct {
	name string
	bus  *Bus

	mu   sync.Mutex
	pubs publisherSet
	sub  *subscribeState // lazily created on first subscribe
	stop stopFlag        // signaled on Close
}

// Name returns the client's human-readable name.
func (c *Client) Name() string { return c.name }

// Close closes the client. It implicitly closes all publishers and
// subscribers obtained from this client.
```
