---
description: Source module pkg/taui/statusrow_test.go (51 lines).
resource: pkg/taui/statusrow_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: statusrow_test.go
type: Module
---

# Module statusrow_test.go

**Path**: `pkg/taui/statusrow_test.go`  
**Lines**: 51

## Snippet Preview

```
package taui

import (
	"strings"
	"testing"
)

func TestStatusRowStates(t *testing.T) {
	cases := []struct {
		state     StatusRowState
		wantGlyph string
	}{
		{StatusRowRunning, ""}, // spinner frame varies; checked separately
		{StatusRowSuccess, "✓"},
		{StatusRowFailed, "✗"},
		{StatusRowNeutral, "•"},
	}
	for _, tc := range cases {
		r := NewStatusRow("label", "detail", tc.state)
		line := r.Render(80)[0]
		if !strings.Contains(line, "label") || !strings.Contains(line, "detail") {
			t.Errorf("state %d: line %q missing label/detail", tc.state, line)
		}
		if tc.wantGlyph != "" && !strings.Contains(line, tc.wantGlyph) {
			t.Errorf("state %d: line %q missing glyph %q", tc.state, line, tc.wantGlyph)
		}
	}
}

func TestStatusRowNoDetailOmitsSeparator(t *testing.T) {
```
