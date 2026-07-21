---
description: Source module pkg/taui/lineinput_test.go (292 lines).
resource: pkg/taui/lineinput_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: lineinput_test.go
type: Module
---

# Module lineinput_test.go

**Path**: `pkg/taui/lineinput_test.go`  
**Lines**: 292

## Snippet Preview

```
package taui

import (
	"strings"
	"testing"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

func TestLineInputTypingAndValue(t *testing.T) {
	li := NewLineInput("› ")
	for _, k := range []string{"h", "e", "l", "l", "o"} {
		if !li.HandleInput(k) {
			t.Fatalf("HandleInput(%q) not consumed", k)
		}
	}
	if got := li.Value(); got != "hello" {
		t.Errorf("Value = %q, want %q", got, "hello")
	}
}

func TestLineInputBackspace(t *testing.T) {
	li := NewLineInput("")
	for _, k := range []string{"a", "b", "c"} {
		li.HandleInput(k)
	}
	li.HandleInput("\x7f") // backspace
	if got := li.Value(); got != "ab" {
		t.Errorf("after backspace Value = %q, want %q", got, "ab")
	}
```
