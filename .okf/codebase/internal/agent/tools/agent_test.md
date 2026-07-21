---
description: Source module internal/agent/tools/agent_test.go (1122 lines).
resource: internal/agent/tools/agent_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: agent_test.go
type: Module
---

# Module agent_test.go

**Path**: `internal/agent/tools/agent_test.go`  
**Lines**: 1122

## Snippet Preview

```
package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/agent/spec"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/store"
	"github.com/stretchr/testify/require"
)

// TestExecuteAgentTool_SpecNotFound verifies that calling the agent tool with
// a nonexistent spec returns a failed result with no side effects.
func TestExecuteAgentTool_SpecNotFound(t *testing.T) {
	cfg := AgentToolConfig{
		CWD:              t.TempDir(),
		Agents:           config.DefaultAgentsConfig(),
		ParentDepth:      0,
		Bus:              eventbus.New(),
		ParentInstanceID: "tau#root000",
	}
```
