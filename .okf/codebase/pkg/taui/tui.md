---
description: Source module pkg/taui/tui.go (78 lines).
resource: pkg/taui/tui.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: tui.go
type: Module
---

# Module tui.go

**Path**: `pkg/taui/tui.go`  
**Lines**: 78

## Snippet Preview

```
// Package tui provides a minimal component-based terminal UI framework,
// ported from Pi's TUI (packages/tui/src/). It supports differential
// rendering, focus management, overlays, and a growing set of components.
package taui

// Component is the interface all UI components must implement.
type Component interface {
	// Render returns lines for the given viewport width.
	Render(width int) []string

	// Invalidate clears any cached rendering state.
	Invalidate()
}

// InputHandler is an optional interface for components that handle keyboard input.
// When the focused component implements InputHandler, the TUI forwards a single
// discrete key sequence to it. Return true if the input was consumed.
type InputHandler interface {
	HandleInput(data string) bool
}

// PasteHandler is an optional interface for components that want bracketed-paste
// payloads delivered atomically, separate from individual keypresses. The
// content has its \x1b[200~ / \x1b[201~ markers stripped. Return true if the
// paste was consumed.
type PasteHandler interface {
	HandlePaste(content string) bool
}

// Focusable is implemented by components that can receive a hardware cursor.
```
