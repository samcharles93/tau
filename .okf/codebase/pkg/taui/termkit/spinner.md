---
description: Source module pkg/taui/termkit/spinner.go (10 lines).
resource: pkg/taui/termkit/spinner.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: spinner.go
type: Module
---

# Module spinner.go

**Path**: `pkg/taui/termkit/spinner.go`  
**Lines**: 10

## Snippet Preview

```
package termkit

// SpinnerFrames is a smooth braille spinner. Each frame is a single cell wide,
// so overwriting with \r keeps the line stable.
var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// SpinnerFrame returns the frame at the given tick index, wrapping around.
func SpinnerFrame(tick int) string {
	return SpinnerFrames[tick%len(SpinnerFrames)]
}
```
