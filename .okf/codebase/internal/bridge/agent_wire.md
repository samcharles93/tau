---
description: Source module internal/bridge/agent_wire.go (242 lines).
resource: internal/bridge/agent_wire.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: agent_wire.go
type: Module
---

# Module agent_wire.go

**Path**: `internal/bridge/agent_wire.go`  
**Lines**: 242

## Snippet Preview

```
package bridge

import (
	"encoding/json"
	"reflect"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

// ---- Agent wire types (docs/specs/agents/03-wire-protocol.md) ----

// AgentReady is the first message a child process writes on stdout
// to announce itself and its protocol version.
type AgentReady struct {
	Instance string `json:"instance"`
	PID      int    `json:"pid"`
	Protocol int    `json:"protocol"`
}

// AgentAssign is the task assignment the parent sends to the child
// after receiving AgentReady.
type AgentAssign struct {
	TaskID     string         `json:"task_id"`
	InstanceID string         `json:"instance_id"`
	SessionID  string         `json:"session_id"`
	Prompt     string         `json:"prompt"`
	Context    string         `json:"context,omitempty"`
	Model      AgentModelPair `json:"model"`
	Tools      []string       `json:"tools,omitempty"`
	Depth      int            `json:"depth"`
```
