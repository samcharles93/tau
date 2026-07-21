---
description: Source module internal/agent/stdio/transport.go (224 lines).
resource: internal/agent/stdio/transport.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: transport.go
type: Module
---

# Module transport.go

**Path**: `internal/agent/stdio/transport.go`  
**Lines**: 224

## Snippet Preview

```
// Package stdio implements the agent-wire stdio JSONL transport defined in
// docs/specs/agents/03-wire-protocol.md. It provides line-framed
// reader/writer pairs with a 8 MiB line cap, handshake enforcement
// (first message must be agent.ready with a matching protocol version),
// and EOF semantics for both parent and child sides.
package stdio

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// ProtocolVersion is the current agent-wire protocol version. The parent
// checks this against the child's agent.ready at handshake time. Additive
// payload fields do not bump it; structural handshake changes do.
const ProtocolVersion = 1

// MaxLineSize is the maximum encoded envelope size (8 MiB). Anything larger
// belongs in the store, not on the wire (data-plane rule, see 04-storage).
const MaxLineSize = 8 * 1024 * 1024

// ErrLineTooLong is returned when a line exceeds MaxLineSize.
var ErrLineTooLong = errors.New("line exceeds maximum size (8 MiB)")

// ErrHandshakeFailed is returned when the first message from the child is
// not a valid agent.ready or has an unsupported protocol version.
```
