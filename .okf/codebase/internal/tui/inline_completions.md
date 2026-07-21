---
description: Source module internal/tui/inline_completions.go (335 lines).
resource: internal/tui/inline_completions.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: inline_completions.go
type: Module
---

# Module inline_completions.go

**Path**: `internal/tui/inline_completions.go`  
**Lines**: 335

## Snippet Preview

```
package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/pkg/taui"
)

// completionSet is the dynamic completion provider for the inline input. It
// emits taui.CompletionSet values; the Completions widget does the fuzzy
// filtering against the token under the cursor. Slash commands resolve their
// command name first, then argument candidates per command.
func (c *inlineChat) completionSet(ctx taui.CompletionContext) *taui.CompletionSet {
	full := []rune(ctx.Text)
	cur := min(ctx.Cursor, len(full))
	before := string(full[:cur])
	if !strings.HasPrefix(before, "/") {
		return nil
	}

	endsWithSpace := strings.HasSuffix(before, " ")
	fields := strings.Fields(before)

	// The token under the cursor (empty when a space was just typed, meaning a
	// fresh argument slot). replaceStart marks where a chosen completion is
	// spliced in - the start of that token, or the cursor for an empty slot.
```
