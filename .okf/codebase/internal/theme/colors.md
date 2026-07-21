---
description: Source module internal/theme/colors.go (178 lines).
resource: internal/theme/colors.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: colors.go
type: Module
---

# Module colors.go

**Path**: `internal/theme/colors.go`  
**Lines**: 178

## Snippet Preview

```
// Package theme provides Tau's semantic color palette and styling constants.
//
// All color values live here so the rest of the codebase imports them rather
// than repeating hex literals or RGB triples.
package theme

import "github.com/samcharles93/tau/pkg/taui/termkit"

// ToolStatus describes the background and foreground colors for one of the
// three tool-lifecycle states shown in the inline TUI.
type ToolStatus struct {
	BG termkit.Color
	FG termkit.Color
}

// ToolBoxStyle combines background, foreground, and border colors for a
// tool or skill's visual container in the TUI.
type ToolBoxStyle struct {
	BG     termkit.Color
	FG     termkit.Color
	Border termkit.Color
}

var (
	// ToolRunning is the warm peach state shown while a tool is executing.
	ToolRunning = ToolStatus{
		BG: termkit.Color{252, 214, 187},
		FG: termkit.Color{124, 11, 11},
	}

```
