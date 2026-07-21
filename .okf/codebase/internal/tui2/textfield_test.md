---
description: Source module internal/tui2/textfield_test.go (125 lines).
resource: internal/tui2/textfield_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: textfield_test.go
type: Module
---

# Module textfield_test.go

**Path**: `internal/tui2/textfield_test.go`  
**Lines**: 125

## Snippet Preview

```
package tui2

import "testing"

func TestTextFieldInsertAndValue(t *testing.T) {
	f := newTextField("")
	f.Insert("hello")
	if got := f.Value(); got != "hello" {
		t.Fatalf("Value() = %q, want %q", got, "hello")
	}
	if f.cursor != 5 {
		t.Fatalf("cursor = %d, want 5", f.cursor)
	}
}

func TestTextFieldInsertAtCursorMidString(t *testing.T) {
	f := newTextField("")
	f.SetValue("helloworld")
	f.MoveCursor(-5) // cursor now between "hello" and "world"
	f.Insert(" ")
	if got := f.Value(); got != "hello world" {
		t.Fatalf("Value() = %q, want %q", got, "hello world")
	}
}

func TestTextFieldBackspace(t *testing.T) {
	f := newTextField("")
	f.SetValue("abc")
	f.Backspace()
	if got := f.Value(); got != "ab" {
```
