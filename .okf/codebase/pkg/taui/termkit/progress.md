---
description: Source module pkg/taui/termkit/progress.go (33 lines).
resource: pkg/taui/termkit/progress.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: progress.go
type: Module
---

# Module progress.go

**Path**: `pkg/taui/termkit/progress.go`  
**Lines**: 33

## Snippet Preview

```
package termkit

import "strings"

// ProgressBar renders an N-wide bar using partial block characters for sub-cell
// resolution (▏▎▍▌▋▊▉█), so motion looks smooth even at low widths.
func ProgressBar(fraction float64, width int) string {
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	blocks := []rune("▏▎▍▌▋▊▉█")
	full := fraction * float64(width)
	whole := int(full)
	var b strings.Builder
	for i := 0; i < whole && i < width; i++ {
		b.WriteRune('█')
	}
	if whole < width {
		frac := full - float64(whole)
		idx := int(frac * float64(len(blocks)))
		if idx > 0 {
			b.WriteRune(blocks[idx-1])
			whole++
		}
	}
	for i := whole; i < width; i++ {
		b.WriteRune(' ')
```
