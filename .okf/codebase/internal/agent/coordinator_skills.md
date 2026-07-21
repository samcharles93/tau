---
description: Source module internal/agent/coordinator_skills.go (314 lines).
resource: internal/agent/coordinator_skills.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: coordinator_skills.go
type: Module
---

# Module coordinator_skills.go

**Path**: `internal/agent/coordinator_skills.go`  
**Lines**: 314

## Snippet Preview

```
package agent

import (
	"fmt"
	"strings"
	"time"

	agentspec "github.com/samcharles93/tau/internal/agent/spec"
	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/skills"
)

// handleRunSkill activates a skill by name in response to a user-invoked
// /skill:<name> command. It looks the skill up in the catalog, records the
// activation in the tracker, applies any AllowedTools restriction, injects
// the skill's instructions into the session system prompt so the model has
// them for subsequent turns, and emits Skill-tool-style started/completed
// events so the TUI renders the lilac "loaded" box.
func (c *Coordinator) handleRunSkill(cmd chat.RunSkillCommand) {
	now := normalizedTime(cmd.RequestedAt)

	if c.skillsMgr == nil {
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    "skill manager unavailable",
			Fatal:      false,
			OccurredAt: now,
		})
		return
```
