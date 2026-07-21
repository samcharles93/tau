---
description: Source module pkg/taui/stdin_buffer_test.go (130 lines).
resource: pkg/taui/stdin_buffer_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: stdin_buffer_test.go
type: Module
---

# Module stdin_buffer_test.go

**Path**: `pkg/taui/stdin_buffer_test.go`  
**Lines**: 130

## Snippet Preview

```
package taui

import (
	"reflect"
	"testing"
)

// collect drives a stdinBuffer over the given chunks and returns the keys and
// pastes it emitted, in order.
func collect(chunks ...string) (keys []string, pastes []string) {
	b := newStdinBuffer(
		func(seq string) { keys = append(keys, seq) },
		func(content string) { pastes = append(pastes, content) },
	)
	for _, c := range chunks {
		b.process(c)
	}
	return keys, pastes
}

func TestSplitsBatchedKeys(t *testing.T) {
	// The bug: "1q" arriving in one read matched neither "1" nor "q".
	keys, pastes := collect("1q")
	if want := []string{"1", "q"}; !reflect.DeepEqual(keys, want) {
		t.Errorf("keys = %q, want %q", keys, want)
	}
	if len(pastes) != 0 {
		t.Errorf("unexpected pastes: %q", pastes)
	}
}
```
