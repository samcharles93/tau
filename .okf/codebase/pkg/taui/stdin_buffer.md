---
description: Source module pkg/taui/stdin_buffer.go (346 lines).
resource: pkg/taui/stdin_buffer.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: stdin_buffer.go
type: Module
---

# Module stdin_buffer.go

**Path**: `pkg/taui/stdin_buffer.go`  
**Lines**: 346

## Snippet Preview

```
package taui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	escByte         = '\x1b'
	bracketPasteOn  = "\x1b[200~"
	bracketPasteOff = "\x1b[201~"
)

// Kitty keyboard-protocol modifier bits (the wire value is 1 + this bitmask).
const (
	kittyModShift = 1
	kittyModAlt   = 2
	kittyModCtrl  = 4
	// Lock modifiers must be ignored: a terminal with NumLock/CapsLock active
	// (e.g. ghostty) folds them into the modifier field, so Ctrl arrives as
	// "1+4+128"=133 rather than "1+4"=5. Masking them out keeps key matching
	// independent of lock state.
	kittyLockMods = 64 | 128 // CapsLock | NumLock
)

// canonicalKey normalizes Kitty keyboard-protocol key events back to the legacy
// bytes/sequences the rest of the codebase matches on, regardless of whether the
```
