---
description: Source module internal/tui2/completions.go (618 lines).
resource: internal/tui2/completions.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: completions.go
type: Module
---

# Module completions.go

**Path**: `internal/tui2/completions.go`  
**Lines**: 618

## Snippet Preview

```
package tui2

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/providers"
)

// --- completion types ------------------------------------------------------

// compMatch is a single completion candidate.
type compMatch struct {
	Word        string
	Description string
	// RequiresArg is true for slash commands whose usage is a required
	// argument (e.g. "<id>", "<prompt>") - accepting one of these must not
	// auto-submit, since the command is invalid without more typing. Left
	// false for optional-argument/no-argument commands and for every
	// argument-value completion (a model ID, session ID, etc.), which are
	// leaf values that already make the command valid to run.
	RequiresArg bool
}
```
