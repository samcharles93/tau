---
description: Source module internal/tui2/wrap.go (69 lines).
resource: internal/tui2/wrap.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: wrap.go
type: Module
---

# Module wrap.go

**Path**: `internal/tui2/wrap.go`  
**Lines**: 69

## Snippet Preview

```
package tui2

import "strings"

// wrapWords greedily word-wraps text to maxWidth visible columns,
// preserving explicit newlines as hard breaks and internal whitespace
// as-is (multiple spaces are not collapsed). A single word longer than
// maxWidth is left intact (callers truncate elsewhere if needed).
//
// Mirrors pkg/taui's wrapWords (paragraph.go) but uses visibleWidth
// (statusbar.go) which delegates to lipgloss.Width - the same
// uniseg-based measurement the viewport uses, so wrapped lines fit.
func wrapWords(text string, maxWidth int) []string {
	if maxWidth <= 0 {
		maxWidth = 80
	}
	var out []string
	for para := range strings.SplitSeq(text, "\n") {
		words, spaces := splitPreserving(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := ""
		lineWidth := 0
		for i, w := range words {
			ww := visibleWidth(w)
			switch {
			case line == "":
				line, lineWidth = w, ww
```
