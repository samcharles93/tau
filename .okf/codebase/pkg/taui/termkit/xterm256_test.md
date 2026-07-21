---
description: Source module pkg/taui/termkit/xterm256_test.go (25 lines).
resource: pkg/taui/termkit/xterm256_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: xterm256_test.go
type: Module
---

# Module xterm256_test.go

**Path**: `pkg/taui/termkit/xterm256_test.go`  
**Lines**: 25

## Snippet Preview

```
package termkit

import "testing"

func TestXterm256(t *testing.T) {
	cases := []struct {
		idx  uint8
		want Color
	}{
		{0, Color{0, 0, 0}},
		{15, Color{255, 255, 255}},
		{16, Color{0, 0, 0}},
		{209, Color{255, 135, 95}}, // Shell mode accent
		{134, Color{175, 95, 215}}, // Planning mode accent
		{215, Color{255, 175, 95}}, // distinct from 209 - regression guard
		{231, Color{255, 255, 255}},
		{232, Color{8, 8, 8}},
		{255, Color{238, 238, 238}},
	}
	for _, tc := range cases {
		if got := Xterm256(tc.idx); got != tc.want {
			t.Errorf("Xterm256(%d) = %v, want %v", tc.idx, got, tc.want)
		}
	}
}
```
