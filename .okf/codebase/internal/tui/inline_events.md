---
description: Source module internal/tui/inline_events.go (563 lines).
resource: internal/tui/inline_events.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: inline_events.go
type: Module
---

# Module inline_events.go

**Path**: `internal/tui/inline_events.go`  
**Lines**: 563

## Snippet Preview

```
package tui

import (
	"fmt"
	"strings"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/internal/tui/notify"
	"github.com/samcharles93/tau/pkg/taui"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// eventLoop bridges runtime chat events into the inline UI until the session
// closes.
func (c *inlineChat) eventLoop() {
	defer c.runtime.Close()
	for {
		select {
		case <-c.done:
			return
		case ev, ok := <-c.sub.Events():
			if !ok {
				return
			}
			c.handleEvent(ev)
		}
	}
}
```
