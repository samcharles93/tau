---
description: Source module internal/tui2/commands.go (839 lines).
resource: internal/tui2/commands.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: commands.go
type: Module
---

# Module commands.go

**Path**: `internal/tui2/commands.go`  
**Lines**: 839

## Snippet Preview

```
package tui2

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	agentspec "github.com/samcharles93/tau/internal/agent/spec"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/providers"
	"github.com/samcharles93/tau/internal/providerui"
)

// --- command table ---------------------------------------------------------

// slashEntry is one row in the tui2 command table. It mirrors the legacy
// slashCommand type but handlers return tea.Cmd instead of mutating directly.
type slashEntry struct {
	name        string
	aliases     []string
	usage       string
	displayName string
	description string
	isAgent     bool
```
