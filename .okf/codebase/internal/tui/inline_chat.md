---
description: Source module internal/tui/inline_chat.go (968 lines).
resource: internal/tui/inline_chat.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: inline_chat.go
type: Module
---

# Module inline_chat.go

**Path**: `internal/tui/inline_chat.go`  
**Lines**: 968

## Snippet Preview

```
package tui

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/metrics"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/internal/tui/notify"
	"github.com/samcharles93/tau/pkg/taui"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// headerIdle is the header text when no turn is running. It is intentionally
// blank - the bottom status line carries the τ tau · model · provider context,
// and the header slot is reused to show the working indicator during a turn.
const headerIdle = ""

// quitConfirmWindow is how long a second Ctrl+C is honored as "confirm quit"
// after the first. The status-bar hint's Duration must match this exactly so
// the hint never outlives (or undersells) the armed window.
const quitConfirmWindow = 800 * time.Millisecond

```
