---
description: Source module internal/bridge/bridge.go (340 lines).
resource: internal/bridge/bridge.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: bridge.go
type: Module
---

# Module bridge.go

**Path**: `internal/bridge/bridge.go`  
**Lines**: 340

## Snippet Preview

```
package bridge

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/skills"
)

// Runtime is the coordinator side of the bridge: it accepts commands.
type Runtime interface {
	Send(cmd tauchat.ChatCommand) error
	Close()
}

// InitInfo is sent to every browser on WebSocket connection.
type InitInfo struct {
	SessionID         string
	Model             string
	Provider          string
	Models            []tauchat.ChatModelRef
	Providers         []string
	Commands          []tauchat.CommandRef
```
