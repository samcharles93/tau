---
description: Source module pkg/taui/termkit/color.go (199 lines).
resource: pkg/taui/termkit/color.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: color.go
type: Module
---

# Module color.go

**Path**: `pkg/taui/termkit/color.go`  
**Lines**: 199

## Snippet Preview

```
// Package termkit provides zero-dependency ANSI terminal rendering primitives.
// It is designed for CLI/TUI apps that need polished inline output - spinners,
// progress bars, hyperlinks, and three-state tool-call lifecycles - without
// coupling to any particular TUI framework.
//
// The package respects NO_COLOR (https://no-color.org) and automatically
// degrades to plain text when stdout is not a character device.
package termkit

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// ─────────────────────────────────────────────────────────────────────────────
// ANSI escape sequences - CSI ("\033[") and OSC ("\033]")
// ─────────────────────────────────────────────────────────────────────────────

const (
	Reset     = "\033[0m"
	Bold      = "\033[1m"
	Dim       = "\033[2m"
	Italic    = "\033[3m"
	Underline = "\033[4m"
)

// Color is an RGB triple for truecolor SGR sequences.
```
