---
description: Source module internal/agent/spec/discover.go (154 lines).
resource: internal/agent/spec/discover.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: discover.go
type: Module
---

# Module discover.go

**Path**: `internal/agent/spec/discover.go`  
**Lines**: 154

## Snippet Preview

```
package spec

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/samcharles93/tau/internal/skills"
)

// agentFileSuffix is the extension a filesystem-loaded agent definition must
// use, matching the embedded built-in template naming (e.g. "plan.agent.md").
const agentFileSuffix = ".agent.md"

// Source describes one root directory to scan for agent definitions.
type Source struct {
	Root     string
	Scope    skills.Scope
	Priority int
}

const (
	userSourcePriority    = 10
	projectSourcePriority = 20
)

// Diagnostic reports a parse or filesystem issue discovered while loading
// agent definitions from disk. Unlike a hard error, a Diagnostic never stops
```
