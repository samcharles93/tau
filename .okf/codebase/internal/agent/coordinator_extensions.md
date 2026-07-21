---
description: Source module internal/agent/coordinator_extensions.go (124 lines).
resource: internal/agent/coordinator_extensions.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: coordinator_extensions.go
type: Module
---

# Module coordinator_extensions.go

**Path**: `internal/agent/coordinator_extensions.go`  
**Lines**: 124

## Snippet Preview

```
package agent

import (
	"strings"
	"time"

	"github.com/samcharles93/tau/internal/chat"
)

func (c *Coordinator) handleReloadExtensions(cmd chat.ReloadExtensionsCommand) {
	now := normalizedTime(cmd.RequestedAt)
	if c.extensionReloader == nil {
		c.emit(chat.ChatNotificationEvent{
			Message:    "Extension reload is not available",
			Level:      chat.ChatNotificationWarn,
			OccurredAt: now,
		})
		return
	}

	idle := c.isIdle()
	if !idle {
		c.emit(chat.ChatNotificationEvent{
			Message:    "Extension reload is only available while idle",
			Level:      chat.ChatNotificationWarn,
			OccurredAt: now,
		})
		return
	}

```
