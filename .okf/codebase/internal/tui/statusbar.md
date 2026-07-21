---
description: Source module internal/tui/statusbar.go (200 lines).
resource: internal/tui/statusbar.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: statusbar.go
type: Module
---

# Module statusbar.go

**Path**: `internal/tui/statusbar.go`  
**Lines**: 200

## Snippet Preview

```
package tui

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/samcharles93/tau/pkg/taui"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// statusSeg is one widget in the status bar. text is the PLAIN string (used for
// width math); style applies ANSI styling and is nil for the default grey. prio
// orders width-pressure dropping within the right group: lower prios are dropped
// first, prioTransient is never dropped.
//
// When styledOverride is non-empty it replaces style(text) in the styled output
// (text is still used for width measurement). This allows embedding raw ANSI
// sequences - e.g. OSC 8 hyperlinks - that can't be produced by applying style
// to text alone.
type statusSeg struct {
	text           string
	style          func(string) string
	prio           int
	styledOverride string
}

// Drop priorities for right-group segments. Lower is dropped first under width
// pressure; transient items (steering / notifications) are never dropped.
```
