---
description: Source module internal/tui2/prompt.go (287 lines).
resource: internal/tui2/prompt.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: prompt.go
type: Module
---

# Module prompt.go

**Path**: `internal/tui2/prompt.go`  
**Lines**: 287

## Snippet Preview

```
package tui2

import (
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

// promptKind distinguishes a yes/no confirmation from a free-text question.
type promptKind int

const (
	promptConfirm promptKind = iota
	promptQuestion
)

// formPrompt is tui2's one reusable modal-input widget: a title, message,
// and either a Yes/No toggle (confirm) or a *textField (question). It's
// deliberately not tied to the agent-question protocol - resolving it just
// invokes a plain Go callback - so the same widget backs both:
//   - agent-originated prompts (enqueuePrompt adapts an incoming
//     InteractivePromptRequestedEvent into a formPrompt whose callbacks send
//     a RespondInteractivePromptCommand back to the runtime), and
//   - local UI flows with no chat round-trip at all (see presentLocalPrompt,
//     used by providerLogin's GitHub Copilot Enterprise-domain prompt).
```
