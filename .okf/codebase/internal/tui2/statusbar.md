---
description: Source module internal/tui2/statusbar.go (631 lines).
resource: internal/tui2/statusbar.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: statusbar.go
type: Module
---

# Module statusbar.go

**Path**: `internal/tui2/statusbar.go`  
**Lines**: 631

## Snippet Preview

```
package tui2

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// --- agent state -------------------------------------------------------------

// agentState is an explicit, closed set of what the agent is currently
// doing - the status bar's single source of truth for which content to
// show, instead of inferring "what's happening" by sniffing
// m.notification text or combining inResponse/streaming/tools ad hoc at
// render time. events.go's handleChatEvent sets this at each transition;
// computeStatusBar only ever reads it.
type agentState int

const (
	// agentReady is the zero value - correct for a freshly constructed
	// model with no turn yet in flight, and restored once a turn completes.
	agentReady agentState = iota
	agentThinking
```
