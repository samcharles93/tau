---
description: Source module internal/agent/instantiate.go (367 lines).
resource: internal/agent/instantiate.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: instantiate.go
type: Module
---

# Module instantiate.go

**Path**: `internal/agent/instantiate.go`  
**Lines**: 367

## Snippet Preview

```
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/samcharles93/tau/internal/agent/spec"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/procid"
	"github.com/samcharles93/tau/internal/store"
)

// InstantiateConfig holds the parameters for bringing an agent identity
// into existence. See docs/specs/agents/02-spawning-and-lifecycle.md.
type InstantiateConfig struct {
	// Name is the spec to resolve: bare "tau", "research", or prefixed
	// "user:" / "project:".
	Name string
	// CWD is the working directory for spec discovery and instance context.
	CWD string
	// ParentInstanceID is set for children, empty for root processes.
	ParentInstanceID string
	// ParentSessionID is set for children, empty for root processes.
	ParentSessionID string
	// ParentDepth is the parent's depth; child depth = parentDepth + 1.
```
