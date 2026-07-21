---
description: Source module internal/tui2/commands_test.go (285 lines).
resource: internal/tui2/commands_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: commands_test.go
type: Module
---

# Module commands_test.go

**Path**: `internal/tui2/commands_test.go`  
**Lines**: 285

## Snippet Preview

```
package tui2

import (
	"context"
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

func TestParseCopyCount(t *testing.T) {
	tests := []struct {
		input string
		want  int
		err   bool
	}{
		{"1", 1, false},
		{"5", 5, false},
		{"42", 42, false},
		{"0", 0, true},
		{"-1", 0, true},
		{"abc", 0, true},
		{"", 0, true},
		{" 1 ", 0, true}, // strconv.Atoi rejects leading spaces
	}
	for _, tt := range tests {
		got, err := parseCopyCount(tt.input)
		if (err != nil) != tt.err {
```
