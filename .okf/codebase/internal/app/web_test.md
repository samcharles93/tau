---
description: Source module internal/app/web_test.go (117 lines).
resource: internal/app/web_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: web_test.go
type: Module
---

# Module web_test.go

**Path**: `internal/app/web_test.go`  
**Lines**: 117

## Snippet Preview

```
package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubRuntime struct {
	mu   sync.Mutex
	sent []tauchat.ChatCommand
}

func (r *stubRuntime) Send(cmd tauchat.ChatCommand) error {
	r.mu.Lock()
	r.sent = append(r.sent, cmd)
	r.mu.Unlock()
	return nil
}
```
