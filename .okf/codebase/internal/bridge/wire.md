---
description: Source module internal/bridge/wire.go (190 lines).
resource: internal/bridge/wire.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: wire.go
type: Module
---

# Module wire.go

**Path**: `internal/bridge/wire.go`  
**Lines**: 190

## Snippet Preview

```
// Package bridge provides a WebSocket gateway between browser clients and the
// Tau chat runtime. The wire protocol wraps every chat.ChatEvent and
// chat.ChatCommand in a JSON envelope with a discriminator field so TypeScript
// consumers and Go can unmarshal interface-typed values.
package bridge

import (
	"encoding/json"
	"fmt"
	"reflect"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

// Envelope is the on-the-wire wrapper for every WebSocket message.
// From/To are optional agent-instance addresses used by the agent wire
// protocol (see docs/specs/agents/03-wire-protocol.md). Browser-bound
// messages omit these fields, preserving backwards compatibility.
type Envelope struct {
	Type    string          `json:"type"`
	From    string          `json:"from,omitempty"`
	To      string          `json:"to,omitempty"`
	Payload json.RawMessage `json:"payload"`
}

// EventEnvelope carries a chat.ChatEvent to the browser.
type EventEnvelope struct {
	Type    string            `json:"type"`
	Payload tauchat.ChatEvent `json:"payload"`
}
```
