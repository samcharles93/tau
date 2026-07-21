---
description: Source module pkg/taui/termkit/xterm256.go (53 lines).
resource: pkg/taui/termkit/xterm256.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: xterm256.go
type: Module
---

# Module xterm256.go

**Path**: `pkg/taui/termkit/xterm256.go`  
**Lines**: 53

## Snippet Preview

```
package termkit

// Xterm256 converts a standard xterm 256-color palette index (0-255) to its
// RGB truecolor equivalent, so colors can be specified using the familiar
// palette index (as most terminal color charts document them) instead of a
// hand-typed RGB triple.
//
// Indices 0-15 are the basic/bright ANSI colors (approximated using the
// widely-used xterm default values), 16-231 are the 6x6x6 color cube, and
// 232-255 are the grayscale ramp.
func Xterm256(idx uint8) Color {
	switch {
	case idx < 16:
		return ansiBasic16[idx]
	case idx < 232:
		i := idx - 16
		return Color{cubeLevel(i / 36), cubeLevel((i / 6) % 6), cubeLevel(i % 6)}
	default:
		level := uint8(8 + (idx-232)*10)
		return Color{level, level, level}
	}
}

// cubeLevel maps a 0-5 cube coordinate to its 0-255 intensity, per the
// standard xterm 256-color cube step values (0, 95, 135, 175, 215, 255).
func cubeLevel(n uint8) uint8 {
	if n == 0 {
		return 0
	}
	return 55 + 40*n
```
