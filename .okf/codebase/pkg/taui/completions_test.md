---
description: Source module pkg/taui/completions_test.go (352 lines).
resource: pkg/taui/completions_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: completions_test.go
type: Module
---

# Module completions_test.go

**Path**: `pkg/taui/completions_test.go`  
**Lines**: 352

## Snippet Preview

```
package taui

import (
	"testing"
	"time"
)

func TestCompletionsHiddenByDefault(t *testing.T) {
	input := NewLineInput("")
	c := NewCompletions(input, func(ctx CompletionContext) *CompletionSet {
		return nil
	})
	if c.Visible() {
		t.Fatal("completions should be hidden when provider returns nil")
	}
}

func TestCompletionsShowsWhenProviderReturns(t *testing.T) {
	input := NewLineInput("")
	for _, r := range "fx" {
		input.HandleInput(string(r))
	}
	c := NewCompletions(input, func(ctx CompletionContext) *CompletionSet {
		if ctx.Text == "" {
			return nil
		}
		return &CompletionSet{
			ReplaceStart: 0,
			ReplaceEnd:   len([]rune(ctx.Text)),
			Groups: []MatchGroup{{
```
