---
description: Source module internal/bridge/bridge_test.go (111 lines).
resource: internal/bridge/bridge_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: bridge_test.go
type: Module
---

# Module bridge_test.go

**Path**: `internal/bridge/bridge_test.go`  
**Lines**: 111

## Snippet Preview

```
package bridge

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRuntime struct {
	mu   sync.Mutex
	sent []tauchat.ChatCommand
}

func (r *fakeRuntime) Send(cmd tauchat.ChatCommand) error {
	r.mu.Lock()
	r.sent = append(r.sent, cmd)
	r.mu.Unlock()
	return nil
}

func (r *fakeRuntime) Close() {}

```
