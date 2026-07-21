---
description: Source module internal/agent/tools/skill.go (100 lines).
resource: internal/agent/tools/skill.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: skill.go
type: Module
---

# Module skill.go

**Path**: `internal/agent/tools/skill.go`  
**Lines**: 100

## Snippet Preview

```
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/samcharles93/tau/internal/skills"
)

// RegisterSkillTool registers the "skill" tool into the registry, enabling the
// LLM to look up and activate a named skill from the catalog. When a skill with
// non-empty AllowedTools is activated, the setAllowedTools callback is invoked
// so the coordinator can restrict the available tool set in subsequent turns.
func RegisterSkillTool(reg *Registry, skillsMgr *skills.Manager, tracker *skills.Tracker, setAllowedTools func([]string)) {
	tool := Tool{
		Schema: Schema{
			Name:        "skill",
			Description: "Activate a skill to get specialised instructions for a task. Call this when a task matches an available skill's description.",
			Parameters:  json.RawMessage(`{"type": "object", "properties": {"name": {"type": "string", "description": "The skill name to activate"}}, "required": ["name"]}`),
		},
		Execute: func(ctx context.Context, params json.RawMessage, ui UIBridge) (Result, error) {
			var args struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(params, &args); err != nil {
				return Result{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}, nil
			}

```
