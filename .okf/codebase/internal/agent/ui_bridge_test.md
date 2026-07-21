---
description: Source module internal/agent/ui_bridge_test.go (40 lines).
resource: internal/agent/ui_bridge_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: ui_bridge_test.go
type: Module
---

# Module ui_bridge_test.go

**Path**: `internal/agent/ui_bridge_test.go`  
**Lines**: 40

## Snippet Preview

```
package agent

import (
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/stretchr/testify/require"
)

func TestCoordinatorUIBridge_Log(t *testing.T) {
	bus := eventbus.New()
	t.Cleanup(bus.Close)
	client := bus.Client("test")
	chatPub := eventbus.Publish[chat.ChatEvent](client)

	c := &Coordinator{
		chatPub: chatPub,
	}
	bridge := &coordinatorUIBridge{coordinator: c}

	sub := eventbus.Subscribe[chat.ChatEvent](client)
	defer sub.Close()

	// Wait for subscription to be ready
	time.Sleep(10 * time.Millisecond)

	bridge.Log("test chunk")

```
