---
description: Source module internal/tui2/animation.go (71 lines).
resource: internal/tui2/animation.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: animation.go
type: Module
---

# Module animation.go

**Path**: `internal/tui2/animation.go`  
**Lines**: 71

## Snippet Preview

```
package tui2

import (
	"strconv"
	"time"
)

// This file holds the "living" presentation touches that keep tui2 from
// feeling robotic while a turn is in flight: a calm dot animation for the
// working indicator, a compact elapsed-time formatter for tool rows, and
// the per-state glyphs for tool rows. All of it is driven off the existing
// 80ms spinner tick (see model.spinnerFrame) - no goroutines, no extra
// timers - so it composes through Bubbletea's normal Update/View flow.

// thinkingDotFrames are the three animation frames for the working indicator:
// a single dot, two dots, three dots, then repeat.
var thinkingDotFrames = [...]string{
	"●",
	"●●",
	"●●●",
}

// thinkingDotFrameTicks is how many 80ms spinner ticks each dot frame stays
// visible before advancing. At 6 ticks per frame, each visual frame lasts
// ~480ms, giving a calm, unhurried pulse.
const thinkingDotFrameTicks = 6

// thinkingDots returns the dot frame for a given spinnerFrame index, advancing
// every thinkingDotFrameTicks ticks and wrapping through the three frames.
// Pure and total - safe for any int input, including negative values.
```
