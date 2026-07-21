---
description: Source module internal/tui2/msgs.go (88 lines).
resource: internal/tui2/msgs.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: msgs.go
type: Module
---

# Module msgs.go

**Path**: `internal/tui2/msgs.go`  
**Lines**: 88

## Snippet Preview

```
package tui2

import (
	"time"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
)

// noopCmd performs no action. Used where a handler must always return a
// non-nil Cmd even when there's nothing to schedule.
var noopCmd tea.Cmd = func() tea.Msg { return nil }

// chatEventMsg wraps a ChatEvent for delivery to the Bubbletea update loop.
type chatEventMsg struct {
	event tauchat.ChatEvent
}

// tickMsg is delivered by tea.Tick to drive timed animations (spinner,
// steering dots). Each tick bumps the spinner frame and returns another
// tick while the model is inResponse.
type tickMsg struct {
	t time.Time
}

// chatEventsClosedMsg is delivered when the subscriber channel closes, either
// from subscriber.Done() or from the events channel itself closing (N12).
type chatEventsClosedMsg struct{}
```
