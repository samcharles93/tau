---
description: Source module internal/tui2/childtranscript.go (144 lines).
resource: internal/tui2/childtranscript.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: childtranscript.go
type: Module
---

# Module childtranscript.go

**Path**: `internal/tui2/childtranscript.go`  
**Lines**: 144

## Snippet Preview

```
package tui2

import (
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
)

// childTranscriptViewerState is the state of an open child-agent transcript
// overlay (drill-down into a child's conversation, live or finished). See
// docs/specs/agents/05-ui.md "Drill-down". A nil *childTranscriptViewerState
// on model means none is open - mirrors diffViewerState's nil-sentinel idiom.
type childTranscriptViewerState struct {
	title     string
	sessionID string
	callID    string // the agent tool call ID, for live transcript lookup
	live      bool   // true if child is still running (render from childMessages)
	viewport  viewport.Model
	loading   bool
}

// openChildTranscriptViewer opens the transcript overlay for the child agent
// behind callID. For live children, messages are read from the in-memory
// childMessages buffer; for finished children, the persisted session is loaded
// asynchronously. Returns nil (no overlay, an inline notification instead) if
```
