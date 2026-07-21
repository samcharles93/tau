---
description: Source module internal/sessions/manager.go (162 lines).
resource: internal/sessions/manager.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: manager.go
type: Module
---

# Module manager.go

**Path**: `internal/sessions/manager.go`  
**Lines**: 162

## Snippet Preview

```
// Package sessions provides session persistence CRUD with runtime-config
// merging. It wraps store.SessionStore and handles type conversion between
// store-level and chat-level types, keeping persistence details out of the
// agent coordinator.
package sessions

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/store"
)

// RuntimeSessionConfig carries the runtime-only fields that must be spliced
// into a loaded session state after a store.Load(). These fields are not
// persisted by the store - they are live runtime configuration only.
type RuntimeSessionConfig struct {
	Provider    config.ProviderConfig
	ModelID     string
	ModelURL    string
	ModelConfig config.ModelConfig
	Parameters  chat.ChatParameters
}

// Manager wraps a store.SessionStore and provides CRUD operations that
```
