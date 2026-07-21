---
description: Source module internal/tui2/model.go (829 lines).
resource: internal/tui2/model.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: model.go
type: Module
---

# Module model.go

**Path**: `internal/tui2/model.go`  
**Lines**: 829

## Snippet Preview

```
package tui2

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/metrics"
	"github.com/samcharles93/tau/internal/providers"
	"github.com/samcharles93/tau/internal/providerui"
	"github.com/samcharles93/tau/internal/tui/notify"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// model is the root Bubbletea model for the chat TUI.
type model struct {
	ctx     context.Context
	runtime tauchat.ChatRuntime
	chatSub *eventbus.Subscriber[tauchat.ChatEvent]

	sessionID string
	modelName string
	provider  string
```
