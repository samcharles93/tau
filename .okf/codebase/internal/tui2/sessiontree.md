---
description: Source module internal/tui2/sessiontree.go (220 lines).
resource: internal/tui2/sessiontree.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: sessiontree.go
type: Module
---

# Module sessiontree.go

**Path**: `internal/tui2/sessiontree.go`  
**Lines**: 220

## Snippet Preview

```
package tui2

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

// sessionTreeRow is one flattened row of the Ctrl+O session navigator: a
// session summary plus its nesting depth (from ParentSessionID) and whether
// it's the session currently loaded in this TUI instance.
type sessionTreeRow struct {
	id        string
	label     string
	depth     int
	isActive  bool
	agentInfo string // e.g. "spec_name · instance_id"
}

// sessionTreeState is the state of an open Ctrl+O session navigator - a nil
// *sessionTreeState on model means none is open, same nil-sentinel idiom as
// contextMenu.
type sessionTreeState struct {
	selected int
}
```
