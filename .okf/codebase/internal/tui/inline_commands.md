---
description: Source module internal/tui/inline_commands.go (717 lines).
resource: internal/tui/inline_commands.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: inline_commands.go
type: Module
---

# Module inline_commands.go

**Path**: `internal/tui/inline_commands.go`  
**Lines**: 717

## Snippet Preview

```
package tui

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	agentspec "github.com/samcharles93/tau/internal/agent/spec"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/pkg/taui"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// slashCommand is one entry in the inline chat's command table. The table is the
// single source of truth for dispatch (run), completion (name + complete), and
// the /help listing - they previously drifted as three separate lists.
type slashCommand struct {
	name        string
	aliases     []string
	usage       string // argument hint shown in /help, e.g. "<id>" or "[on|off]"
	description string
	run         func(c *inlineChat, args string)
	complete    completeFunc // nil → command takes no completable arguments
	// isAgent marks a command generated from a built-in agent definition
	// (internal/agent/spec), so the completion dropdown can list it under its
	// own "Agents" group instead of lumping it in with core commands.
```
