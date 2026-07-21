---
description: Source module internal/tui2/tools.go (1277 lines).
resource: internal/tui2/tools.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: tools.go
type: Module
---

# Module tools.go

**Path**: `internal/tui2/tools.go`  
**Lines**: 1277

## Snippet Preview

```
package tui2

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

type toolState struct {
	id     string
	name   string
	args   string
	result string
	status string // "pending", "running", "done", "error"

	// summary is a short, model-authored one-liner describing what this
	// call is doing (see ChatToolExecutionStartedEvent.Summary), shown in
	// the status bar while the tool runs. Empty when the model didn't
	// supply one.
	summary string

	// startedAt is initialized when the call appears as a fallback, then reset
```
