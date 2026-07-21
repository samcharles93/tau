---
description: Source module pkg/taui/taillog_test.go (93 lines).
resource: pkg/taui/taillog_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: taillog_test.go
type: Module
---

# Module taillog_test.go

**Path**: `pkg/taui/taillog_test.go`  
**Lines**: 93

## Snippet Preview

```
package taui

import "testing"

func TestTailLog_AppendSplitsLinesAcrossChunks(t *testing.T) {
	tl := NewTailLog(10, nil)

	tl.Append("hello ")
	tl.Append("world\nsecond line\nthird")

	got := tl.Render(0)
	want := []string{"hello world", "second line", "third"}
	if len(got) != len(want) {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Render()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestTailLog_TrimsCarriageReturn(t *testing.T) {
	tl := NewTailLog(10, nil)
	tl.Append("line one\r\nline two\r\n")

	got := tl.Render(0)
	want := []string{"line one", "line two"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Render() = %q, want %q", got, want)
```
