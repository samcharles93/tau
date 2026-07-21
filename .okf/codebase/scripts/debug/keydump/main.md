---
description: Source module scripts/debug/keydump/main.go (114 lines).
resource: scripts/debug/keydump/main.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: main.go
type: Module
---

# Module main.go

**Path**: `scripts/debug/keydump/main.go`  
**Lines**: 114

## Snippet Preview

```
// Command keydump prints the raw bytes a terminal sends for each keypress, with
// the Kitty keyboard protocol enabled exactly as tau enables it (flag 1). It is
// a debugging aid for terminal key-handling issues - e.g. discovering that a
// terminal folds NumLock/CapsLock into the modifier field.
//
// Run it, press the keys you want to inspect, then press 'q' to quit:
//
//	go run ./scripts/debug/keydump
//
// For CSI-u key events it also decodes the codepoint and active modifiers, so a
// line like:
//
//	hex: 1b5b39393b31333375  raw: "\x1b[99;133u"  key=Ctrl+c  mods=ctrl+numlock
//
// makes it obvious when a lock key is altering the modifier value.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func main() {
	// Raw mode via stty: byte-at-a-time, no echo, no signal translation.
	stty("raw", "-echo")
	defer stty("sane")
```
