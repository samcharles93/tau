---
description: Source module pkg/taui/toolrow_test.go (87 lines).
resource: pkg/taui/toolrow_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: toolrow_test.go
type: Module
---

# Module toolrow_test.go

**Path**: `pkg/taui/toolrow_test.go`  
**Lines**: 87

## Snippet Preview

```
package taui

import (
	"strings"
	"testing"
)

// The default (combined) style shows a ✓/✗ glyph + name/args/detail; the badge
// style shows SUCCESS/FAILED words. Both the glyphs and the name/args/detail
// appear in the colour and no-colour branches, so these assertions are stable
// regardless of whether termkit thinks colour is enabled in the test env.

func TestToolRowStartsRunning(t *testing.T) {
	r := NewToolRow("go", "build ./...")
	if !r.Running() {
		t.Fatalf("new ToolRow should be running, got state %d", r.State())
	}
	line := r.Render(80)[0]
	for _, want := range []string{"go", "build ./..."} {
		if !strings.Contains(line, want) {
			t.Errorf("running line %q missing %q", line, want)
		}
	}
}

func TestToolRowSucceed(t *testing.T) {
	r := NewToolRow("go", "build ./...")
	r.Succeed("done in 1.2s")

	if r.State() != ToolSuccess {
```
