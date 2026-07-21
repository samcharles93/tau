---
description: Source module pkg/taui/termios_windows.go (102 lines).
resource: pkg/taui/termios_windows.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: termios_windows.go
type: Module
---

# Module termios_windows.go

**Path**: `pkg/taui/termios_windows.go`  
**Lines**: 102

## Snippet Preview

```
package taui

import (
	"golang.org/x/sys/windows"
)

// Windows console mode flags (not all are exported by x/sys/windows).
const (
	enableEchoInput          = 0x0004
	enableLineInput          = 0x0002
	enableProcessedInput     = 0x0001
	enableWindowInput        = 0x0008
	enableMouseInput         = 0x0010
	enableInsertMode         = 0x0020
	enableQuickEditMode      = 0x0040
	enableExtendedFlags      = 0x0080
	enableVirtTermInput      = 0x0200
	enableVirtTermProcessing = 0x0004
)

// TermiosState holds the saved console mode on Windows.
type TermiosState struct {
	Mode      uint32
	OutMode   uint32
	Handle    windows.Handle
	OutHandle windows.Handle
}

// MakeRaw puts the Windows console into raw mode.
func MakeRaw(fd uintptr) (*TermiosState, error) {
```
