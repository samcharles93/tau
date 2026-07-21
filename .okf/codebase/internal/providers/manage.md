---
description: Source module internal/providers/manage.go (199 lines).
resource: internal/providers/manage.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: manage.go
type: Module
---

# Module manage.go

**Path**: `internal/providers/manage.go`  
**Lines**: 199

## Snippet Preview

```
package providers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/samcharles93/tau/internal/config"
)

// Manage provides the provider lifecycle operations shared by the CLI, both
// TUIs, and the setup wizard: toggling an env-var/keyless provider on or off,
// completing an OAuth login, logging out, and re-resolving the effective
// provider set. Each method loads state fresh and saves it before returning,
// so a Manage instance carries no session state of its own - the caller
// doesn't need to sequence calls or worry about staleness.
//
// The OAuth device-code flow itself (LoginOAuth, BeginOAuthLogin,
// RefreshOAuth) is not wrapped here: the legacy TUI drives it with a blocking
// goroutine and tui2 drives it with a two-phase Bubbletea Cmd, and those
// async patterns are TUI-specific. LoginComplete only covers the part both
// patterns converge on afterwards: persisting the resulting credentials.
type Manage struct {
	getenv    func(string) string
	loadState func() (State, error)
	saveState func(*State) error
}

// NewManage builds a Manage service. getenv may be nil to use the process
```
