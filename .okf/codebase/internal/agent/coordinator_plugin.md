---
description: Source module internal/agent/coordinator_plugin.go (82 lines).
resource: internal/agent/coordinator_plugin.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: coordinator_plugin.go
type: Module
---

# Module coordinator_plugin.go

**Path**: `internal/agent/coordinator_plugin.go`  
**Lines**: 82

## Snippet Preview

```
package agent

import (
	"encoding/json"
	"sort"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/pkg/plugin/api"
)

// marshalMessages serializes messages for plugin event payloads.
func marshalMessages(msgs []chat.ChatMessage) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		b, _ := json.Marshal(m)
		out[i] = string(b)
	}
	return out
}

// marshalParameters serializes chat parameters for plugin event payloads.
func marshalParameters(p chat.ChatParameters) string {
	b, _ := json.Marshal(p)
	return string(b)
}

// applyPluginMessageModifications applies message injections and removals from a
// plugin EventResponse to the provided session state. It processes removals in
// descending index order to avoid index shifting, then appends injected messages.
// Malformed injected messages are skipped rather than failing the turn.
```
