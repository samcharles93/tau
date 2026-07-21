---
description: Source module internal/tui/inline_commands_test.go (64 lines).
resource: internal/tui/inline_commands_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: inline_commands_test.go
type: Module
---

# Module inline_commands_test.go

**Path**: `internal/tui/inline_commands_test.go`  
**Lines**: 64

## Snippet Preview

```
package tui

import (
	"strings"
	"testing"
)

// TestCommandTableIsSingleSourceOfTruth guards that dispatch (slashByName),
// name completion (commandGroups), and /help all derive from the same table,
// so they can't drift the way the old three separate lists did.
func TestCommandTableIsSingleSourceOfTruth(t *testing.T) {
	c, _ := newTestChat(t)

	completed := map[string]bool{}
	for _, g := range c.commandGroups("/") {
		for _, m := range g.Matches {
			completed[m.Word] = true
		}
	}

	for i := range slashCommands {
		cmd := &slashCommands[i]

		// Dispatch: primary name and every alias resolve to this command.
		if slashByName[cmd.name] != cmd {
			t.Errorf("command %q not indexed by primary name", cmd.name)
		}
		for _, a := range cmd.aliases {
			if slashByName[a] != cmd {
				t.Errorf("alias %q of %q not indexed", a, cmd.name)
```
