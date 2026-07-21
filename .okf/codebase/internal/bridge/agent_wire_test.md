---
description: Source module internal/bridge/agent_wire_test.go (224 lines).
resource: internal/bridge/agent_wire_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: agent_wire_test.go
type: Module
---

# Module agent_wire_test.go

**Path**: `internal/bridge/agent_wire_test.go`  
**Lines**: 224

## Snippet Preview

```
package bridge

import (
	"encoding/json"
	"testing"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/stretchr/testify/require"
)

func TestAgentWireRoundTrip(t *testing.T) {
	t.Run("agent.ready", func(t *testing.T) {
		orig := AgentReady{Instance: "research#k3v9qp", PID: 41172, Protocol: 1}
		raw, err := MarshalAgentMessage(orig, "research#k3v9qp", "")
		require.NoError(t, err)
		msg, from, to, err := UnmarshalAgentMessage(raw)
		require.NoError(t, err)
		require.Equal(t, "agent.ready", envelopeType(raw))
		require.Equal(t, "research#k3v9qp", from)
		require.Equal(t, "", to)
		got, ok := msg.(AgentReady)
		require.True(t, ok)
		require.Equal(t, orig, got)
	})

	t.Run("agent.assign", func(t *testing.T) {
		orig := AgentAssign{
			TaskID:     "t-01",
			InstanceID: "research#k3v9qp",
```
