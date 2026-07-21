---
description: Source module pkg/taui/utils.go (218 lines).
resource: pkg/taui/utils.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: utils.go
type: Module
---

# Module utils.go

**Path**: `pkg/taui/utils.go`  
**Lines**: 218

## Snippet Preview

```
package taui

import (
	"strings"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

// VisibleWidth returns the visible width of a string in terminal columns,
// after stripping ANSI escape sequences. Width is computed with grapheme-aware
// rules (rivo/uniseg), so emoji, CJK, combining marks, and ZWJ sequences are
// measured correctly.
func VisibleWidth(s string) int {
	if len(s) == 0 {
		return 0
	}
	// Fast path: pure ASCII printable (1 column each, no escapes).
	if isPrintableASCII(s) {
		return len(s)
	}
	return uniseg.StringWidth(stripANSI(s))
}

// isPrintableASCII returns true if every byte in s is a printable ASCII char (0x20-0x7e).
func isPrintableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
```
