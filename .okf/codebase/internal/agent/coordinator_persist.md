---
description: Source module internal/agent/coordinator_persist.go (304 lines).
resource: internal/agent/coordinator_persist.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: coordinator_persist.go
type: Module
---

# Module coordinator_persist.go

**Path**: `internal/agent/coordinator_persist.go`  
**Lines**: 304

## Snippet Preview

```
package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/sessions"
)

// persistDefaultsOnUpdate writes default_provider and/or default_model to the
// local .tau.yaml when the user changed provider or model through the UI.
func (c *Coordinator) persistDefaultsOnUpdate(patch chat.ChatSessionPatch, snapshot chat.ChatSessionState) {
	if c.projectDir == "" {
		return
	}
	provider := ""
	model := ""
	if patch.Provider != nil {
		provider = snapshot.ProviderName
	}
	if patch.Model != nil {
		model = snapshot.Model.ID
	}
	if provider == "" && model == "" {
		return
```
