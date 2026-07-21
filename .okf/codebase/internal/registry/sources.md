---
description: Source module internal/registry/sources.go (126 lines).
resource: internal/registry/sources.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: sources.go
type: Module
---

# Module sources.go

**Path**: `internal/registry/sources.go`  
**Lines**: 126

## Snippet Preview

```
package registry

import (
	agentspec "github.com/samcharles93/tau/internal/agent/spec"
	cc "github.com/samcharles93/tau/internal/chat/commands"
	"github.com/samcharles93/tau/internal/skills"
)

// builtinCommands returns the set of built-in slash commands shared across
// TUI and web UI. These are the single source of truth for command registration;
// the TUI's inline_commands.go hardcodes the same set for dispatch but the
// registry feed is what drives the web UI completion menu and /help output.
func builtinCommands(cwd string) []Command {
	cmds := []Command{
		{Name: "model", Label: "/model", Description: "switch model", AcceptsArgs: true},
		{Name: "system", Label: "/system", Description: "set the system prompt", AcceptsArgs: true},
		{Name: "effort", Label: "/effort", Description: "set reasoning effort", AcceptsArgs: true},
		{Name: "session", Label: "/session", Description: "manage saved sessions (list, info, export, delete, load)", AcceptsArgs: true},
		{Name: "resume", Label: "/resume", Description: "resume a saved session", AcceptsArgs: true},
		{Name: "refresh", Label: "/refresh", Description: "re-discover available models", AcceptsArgs: false},
		{Name: "reload", Label: "/reload", Description: "reload extensions", AcceptsArgs: false},
		{Name: "clear", Label: "/clear", Description: "start a fresh session [alias: /new, /reset]", AcceptsArgs: false},
		{Name: "help", Label: "/help", Description: "show available commands", AcceptsArgs: false},
	}
	cmds = append(cmds, agentCommands(cwd)...)
	return cmds
}

// agentCommands adapts tau's built-in agent definitions (internal/agent/spec)
// into registry Commands, skipping any marked user-invocable: false, then
```
