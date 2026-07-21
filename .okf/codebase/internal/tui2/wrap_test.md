---
description: Source module internal/tui2/wrap_test.go (99 lines).
resource: internal/tui2/wrap_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: wrap_test.go
type: Module
---

# Module wrap_test.go

**Path**: `internal/tui2/wrap_test.go`  
**Lines**: 99

## Snippet Preview

```
package tui2

import (
	"reflect"
	"strings"
	"testing"
)

func TestWrapWords(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		maxWidth int
		want     []string
	}{
		{
			name:     "short text fits in one line",
			text:     "hello world",
			maxWidth: 80,
			want:     []string{"hello world"},
		},
		{
			name:     "words wrap at width",
			text:     "hello world this is a test",
			maxWidth: 12,
			want:     []string{"hello world", "this is a", "test"},
		},
		{
			name:     "explicit newlines preserved",
			text:     "hello\nworld",
```
