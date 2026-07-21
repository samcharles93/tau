---
description: Source module internal/tui2/events.go (970 lines).
resource: internal/tui2/events.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: events.go
type: Module
---

# Module events.go

**Path**: `internal/tui2/events.go`  
**Lines**: 970

## Snippet Preview

```
package tui2

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/internal/tui/notify"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// handleChatEvent mutates model state and returns an optional Cmd (e.g. for
// notification-clear timers or auto-responses). It MUST be called from within
// Update so the returned Cmd is properly composed.
func (m *model) handleChatEvent(evt tauchat.ChatEvent) tea.Cmd {
	switch e := evt.(type) {
	case tauchat.ChatSessionSnapshotEvent:
		m.applySnapshot(e)

	case tauchat.ChatResponseStartedEvent:
		m.streaming = ""
		m.reasoning = ""
		m.tools = nil
		m.toolGroupCollapsed = m.toolCallsDefaultCollapsed
```
