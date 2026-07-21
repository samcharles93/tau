---
description: Source module internal/tui/inline_providers.go (246 lines).
resource: internal/tui/inline_providers.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: inline_providers.go
type: Module
---

# Module inline_providers.go

**Path**: `internal/tui/inline_providers.go`  
**Lines**: 246

## Snippet Preview

```
package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/providers"
	"github.com/samcharles93/tau/internal/providerui"
	"github.com/samcharles93/tau/pkg/taui"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// argProvider offers completions for /provider's argument(s): argsBefore==0
// offers the catalog provider IDs directly (a bare "/provider <name>"
// toggles it) plus the "login"/"logout" sub-verbs; argsBefore==1, when the
// first word is "login" or "logout", offers the catalog provider IDs again
// as that sub-verb's argument.
func argProvider(c *inlineChat, fields []string, argsBefore int) (string, []taui.Match) {
	menu := providers.Menu(c.providerCfg(), c.providerState(), nil)
	providerMatches := make([]taui.Match, 0, len(menu))
	for _, e := range menu {
		desc := e.DisplayName
		if e.Enabled {
			desc += " (enabled)"
		}
		providerMatches = append(providerMatches, taui.Match{Word: e.ID, Description: desc})
	}
```
