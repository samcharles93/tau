---
description: Source module internal/agent/coordinator_bash.go (114 lines).
resource: internal/agent/coordinator_bash.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: coordinator_bash.go
type: Module
---

# Module coordinator_bash.go

**Path**: `internal/agent/coordinator_bash.go`  
**Lines**: 114

## Snippet Preview

```
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
)

// handleRunBashCommand runs a "!" (or "!!") bash-mode command entered
// directly at the chat input. It executes the same registered "shell" tool
// the LLM itself uses, outside the normal turn loop, and emits the same
// started/output/completed event trio a real LLM-invoked tool call would -
// so it renders identically in the TUI - before appending the result to
// session history (unless Exclude, the "!!" variant, is set).
func (c *Coordinator) handleRunBashCommand(cmd chat.RunBashCommand) {
	now := normalizedTime(cmd.RequestedAt)

	c.mu.Lock()
	session, ok := c.sessions[cmd.SessionID]
	if !ok || session.state == nil {
		c.mu.Unlock()
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    "session not found",
			Fatal:      false,
			OccurredAt: now,
```
