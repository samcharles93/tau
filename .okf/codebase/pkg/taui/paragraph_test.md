---
description: Source module pkg/taui/paragraph_test.go (50 lines).
resource: pkg/taui/paragraph_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: paragraph_test.go
type: Module
---

# Module paragraph_test.go

**Path**: `pkg/taui/paragraph_test.go`  
**Lines**: 50

## Snippet Preview

```
package taui

import (
	"reflect"
	"strings"
	"testing"
)

func TestParagraphWraps(t *testing.T) {
	p := NewParagraph("the quick brown fox jumps")
	lines := p.Render(11) // width 11 columns
	for _, ln := range lines {
		if VisibleWidth(ln) > 11 {
			t.Errorf("wrapped line exceeds width: %q (%d)", ln, VisibleWidth(ln))
		}
	}
	// Reassembling the words should reproduce the input.
	if got := strings.Join(strings.Fields(strings.Join(lines, " ")), " "); got != "the quick brown fox jumps" {
		t.Errorf("wrapped content lost words: %q", got)
	}
}

func TestParagraphHonorsNewlines(t *testing.T) {
	p := NewParagraph("line one\nline two")
	lines := p.Render(80)
	if want := []string{"line one", "line two"}; !reflect.DeepEqual(lines, want) {
		t.Errorf("lines = %q, want %q", lines, want)
	}
}

```
