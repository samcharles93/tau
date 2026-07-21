---
description: Source module internal/tui2/overlay.go (178 lines).
resource: internal/tui2/overlay.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: overlay.go
type: Module
---

# Module overlay.go

**Path**: `internal/tui2/overlay.go`  
**Lines**: 178

## Snippet Preview

```
package tui2

import (
	tea "charm.land/bubbletea/v2"
)

// overlayID names a Category 2 (modal) slot, per
// docs/specs/state-taxonomy.md. Order matters: overlayPrecedence declares
// dispatch priority once, replacing what used to be a hand-written ladder in
// dispatchKey plus scattered manual "close my siblings" calls at each open
// site (openDiffViewer, openSessionTree, presentPrompt, ...).
//
// Completions is not registered here: it's the one soft overlay and has its
// own explicit call site in dispatchKey between "no exclusive overlay is
// open" and "normal keybindings," which a single combined loop can't
// express. It's listed in the const block only as documentation of the full
// precedence order; no live code reads overlayCompletions at runtime.
type overlayID int

const (
	overlayPrompt overlayID = iota
	overlayHelp
	overlayDiff
	overlayChildTranscript
	overlaySessionTree
	overlayContextMenu
	overlayPalette
	overlayCompletions // soft overlay — documented here, dispatched outside this registry
)

```
