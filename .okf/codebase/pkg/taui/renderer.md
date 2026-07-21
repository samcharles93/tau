---
description: Source module pkg/taui/renderer.go (527 lines).
resource: pkg/taui/renderer.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: renderer.go
type: Module
---

# Module renderer.go

**Path**: `pkg/taui/renderer.go`  
**Lines**: 527

## Snippet Preview

```
package taui

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Config holds user-facing knobs for the TUI engine. It follows tau's
// configuration pattern - a plain struct with defaults for the zero value.
type Config struct {
	// DefaultToolStyle is the ToolStyle applied to ToolRow components created
	// via NewToolRow. ToolStyleCombined (the zero value) composes on coloured
	// boxes; ToolStyleBadge uses bg-chip badges.
	DefaultToolStyle ToolStyle

	// CursorColor is the background colour for the block cursor in LineInput.
	// Zero means use the default (mid-grey, rgb 128/134/150).
	CursorColor [3]uint8
}

// TUI is the main engine - it owns the component tree, the terminal, and the
// differential render loop. Ported from Pi's tui.ts TUI class.
type TUI struct {
	Container
	Terminal Terminal

	// Config holds user-facing options. Safe to read from any goroutine after
```
