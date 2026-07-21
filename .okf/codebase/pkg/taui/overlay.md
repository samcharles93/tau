---
description: Source module pkg/taui/overlay.go (157 lines).
resource: pkg/taui/overlay.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: overlay.go
type: Module
---

# Module overlay.go

**Path**: `pkg/taui/overlay.go`  
**Lines**: 157

## Snippet Preview

```
package taui

import "sync"

// Overlay is a component that can be pushed onto an OverlayStack to take
// priority over a base component while it is active - a Prompt-style dialog,
// a Completions-style dropdown, or any future modal/picker.
type Overlay interface {
	Component
	InputHandler
}

type overlayEntry struct {
	overlay   Overlay
	exclusive bool
}

// OverlayStack routes input to an ordered set of active overlays. It
// generalizes the "if activePrompt != nil {...} else if
// completions.HandleInput(...) {...}" chain that otherwise grows one
// hand-written branch per modal-like widget: adding a new overlay becomes a
// Push/Pop call instead of a new branch in the input-dispatch method, and
// HandleInput/HandlePaste forwarding is automatic for every overlay rather
// than something each caller has to remember to wire up individually.
//
// Two overlay flavors are supported, matching what already exists in
// practice:
//
//   - Exclusive overlays (pushed with exclusive=true - e.g. a confirm/
//     question dialog) swallow all input unconditionally once active,
```
