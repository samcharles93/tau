---
description: Source module internal/agent/tools/agent.go (1316 lines).
resource: internal/agent/tools/agent.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: agent.go
type: Module
---

# Module agent.go

**Path**: `internal/agent/tools/agent.go`  
**Lines**: 1316

## Snippet Preview

```
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/samcharles93/tau/internal/agent/spec"
	"github.com/samcharles93/tau/internal/agent/stdio"
	"github.com/samcharles93/tau/internal/bridge"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/procid"
	"github.com/samcharles93/tau/internal/store"
)

// AgentToolConfig holds the dependencies the agent tool executor needs.
type AgentToolConfig struct {
	// CWD is the working directory for child processes.
	CWD string
	// Store is the shared session/instance store.
```
