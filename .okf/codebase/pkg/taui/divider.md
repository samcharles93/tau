---
description: Source module pkg/taui/divider.go (123 lines).
resource: pkg/taui/divider.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: divider.go
type: Module
---

# Module divider.go

**Path**: `pkg/taui/divider.go`  
**Lines**: 123

## Snippet Preview

```
package taui

import (
	"strings"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// modeLabelLeftMargin is how many rule dashes precede a mode's label, e.g.
// "──── Shell ──────...". Unlike a static Divider label (which is centered),
// a mode label sits near the left edge - it's an indicator meant to catch
// the eye immediately, not a centered section heading.
const modeLabelLeftMargin = 4

// InputMode is a named, colored mode a Divider can render itself as - e.g.
// "Shell" while a "!" bash command is being typed, or "Planning" while
// typing "/plan". Color is used both as the rule foreground and as the
// background behind the label.
type InputMode struct {
	Label string
	Color termkit.Color
}

// Divider renders a full-width horizontal rule, with an optional centered
// label. If SetModeFunc is given, the label and color are resolved fresh on
// every render from the current mode instead of the static label passed to
// NewDivider - see SetModeFunc.
type Divider struct {
	label       string
	modeFn      func() *InputMode
```
