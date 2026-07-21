---
description: Source module internal/chat/id.go (49 lines).
resource: internal/chat/id.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: id.go
type: Module
---

# Module id.go

**Path**: `internal/chat/id.go`  
**Lines**: 49

## Snippet Preview

```
package chat

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// NewID mints a UUIDv7 identifier: time-ordered, with enough random low
// bits that concurrent callers don't collide. This is the single
// implementation behind every session/message/request ID in the codebase -
// internal/app, the legacy TUI, and tui2 each used to define their own copy
// of "uuid.NewV7, fall back to a timestamp on error"; internal/agent/tools
// once minted child agent session IDs as a bare time.Now().UnixNano() with
// no randomness at all, which collided under concurrent spawns (two
// children starting on the same clock tick got the identical session ID,
// and the store silently merged one child's transcript into the other's).
// Route every ID through here so that class of bug has one fix, not four.
func NewID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("chat: generating id: %w", err)
	}
	return id.String(), nil
}

// NewMessageID mints a chat message ID. Falls back to a nanosecond
// timestamp if the platform's random source is unavailable, rather than
// propagating the error - it's called from struct literals across the
```
