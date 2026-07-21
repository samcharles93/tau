---
description: Source module internal/agent/coordinator.go (1073 lines).
resource: internal/agent/coordinator.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: coordinator.go
type: Module
---

# Module coordinator.go

**Path**: `internal/agent/coordinator.go`  
**Lines**: 1073

## Snippet Preview

```
// Package agent implements the agent coordinator - the single runtime that
// mediates between the TUI and the LLM. It owns the agentic turn loop:
// stream a completion, detect tool_calls, execute tools in parallel,
// feed results back, and loop until the model produces a final text response.
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	commandreg "github.com/samcharles93/tau/internal/registry"
	"github.com/samcharles93/tau/internal/sessions"
	"github.com/samcharles93/tau/internal/skills"
	"github.com/samcharles93/tau/pkg/plugin/api"
)

const (
	commandBufferSize   = 16
	toolSummaryMaxBytes = 600

```
