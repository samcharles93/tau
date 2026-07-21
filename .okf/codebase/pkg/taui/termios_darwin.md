---
description: Source module pkg/taui/termios_darwin.go (6 lines).
resource: pkg/taui/termios_darwin.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: termios_darwin.go
type: Module
---

# Module termios_darwin.go

**Path**: `pkg/taui/termios_darwin.go`  
**Lines**: 6

## Snippet Preview

```
package taui

import "syscall"

func ioctlGetTermios() uintptr { return syscall.TIOCGETA }
func ioctlSetTermios() uintptr { return syscall.TIOCSETA }
```
