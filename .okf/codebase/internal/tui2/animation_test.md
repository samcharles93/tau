---
description: Source module internal/tui2/animation_test.go (108 lines).
resource: internal/tui2/animation_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: animation_test.go
type: Module
---

# Module animation_test.go

**Path**: `internal/tui2/animation_test.go`  
**Lines**: 108

## Snippet Preview

```
package tui2

import (
	"slices"
	"testing"
	"time"
)

// --- thinkingDots ---

func TestThinkingDotsThreeFrameSequence(t *testing.T) {
	// Frame 0 → "●", frame 6 → "●●", frame 12 → "●●●", frame 18 → "●" (wrap).
	want := []string{"●", "●●", "●●●"}
	for i := range 3 {
		got := thinkingDots(i * thinkingDotFrameTicks)
		if got != want[i] {
			t.Errorf("thinkingDots(%d) = %q, want %q", i*thinkingDotFrameTicks, got, want[i])
		}
	}
}

func TestThinkingDotsWraps(t *testing.T) {
	// After three frames, it should wrap back to the first.
	first := thinkingDots(0)
	wrapped := thinkingDots(3 * thinkingDotFrameTicks)
	if first != wrapped {
		t.Errorf("thinkingDots did not wrap: %q vs %q", first, wrapped)
	}
}

```
