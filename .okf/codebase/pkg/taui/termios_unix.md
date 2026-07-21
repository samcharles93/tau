---
description: Source module pkg/taui/termios_unix.go (61 lines).
resource: pkg/taui/termios_unix.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: termios_unix.go
type: Module
---

# Module termios_unix.go

**Path**: `pkg/taui/termios_unix.go`  
**Lines**: 61

## Snippet Preview

```
//go:build unix

package taui

import (
	"syscall"
	"unsafe"
)

// TermiosState holds the saved terminal settings on Unix systems.
type TermiosState struct {
	Termios syscall.Termios
}

// MakeRaw puts the terminal into raw mode.
func MakeRaw(fd uintptr) (*TermiosState, error) {
	var old syscall.Termios
	if _, _, err := syscall.Syscall6(syscall.SYS_IOCTL, fd, ioctlGetTermios(), uintptr(unsafe.Pointer(&old)), 0, 0, 0); err != 0 {
		return nil, err
	}
	state := &TermiosState{Termios: old}

	raw := old
	// Input flags: disable BRKINT, ICRNL, INPCK, ISTRIP, IXON
	raw.Iflag &^= syscall.BRKINT | syscall.ICRNL | syscall.INPCK | syscall.ISTRIP | syscall.IXON
	// Output flags: disable OPOST
	raw.Oflag &^= syscall.OPOST
	// Control flags: set CS8
	raw.Cflag |= syscall.CS8
	// Local flags: disable ECHO, ICANON, IEXTEN, ISIG
```
