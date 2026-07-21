---
description: Source module pkg/taui/termios_linux.go (6 lines).
resource: pkg/taui/termios_linux.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: termios_linux.go
type: Module
---

# Module termios_linux.go

**Path**: `pkg/taui/termios_linux.go`  
**Lines**: 6

## Snippet Preview

```
package taui

import "syscall"

func ioctlGetTermios() uintptr { return syscall.TCGETS }
func ioctlSetTermios() uintptr { return syscall.TCSETS }
```
