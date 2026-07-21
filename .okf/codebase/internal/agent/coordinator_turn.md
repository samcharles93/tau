---
description: Source module internal/agent/coordinator_turn.go (1419 lines).
resource: internal/agent/coordinator_turn.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: coordinator_turn.go
type: Module
---

# Module coordinator_turn.go

**Path**: `internal/agent/coordinator_turn.go`  
**Lines**: 1419

## Snippet Preview

```
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/pkg/plugin/api"
)

// runTurn is the agentic turn loop. It streams a completion, and if the
// model returns tool_calls, executes them in parallel, appends results
// to the conversation, and loops. Stops when the model produces a final
// text response or an error occurs.
func (c *Coordinator) runTurn(ctx context.Context, state chat.ChatSessionState) {
	sessionID := state.SessionID
	requestID := state.ActiveRequestID
	now := time.Now().UTC()
	turnStartedAt := now

	c.mu.Lock()
```
