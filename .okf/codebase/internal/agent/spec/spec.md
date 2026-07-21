---
description: Source module internal/agent/spec/spec.go (306 lines).
resource: internal/agent/spec/spec.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: spec.go
type: Module
---

# Module spec.go

**Path**: `internal/agent/spec/spec.go`  
**Lines**: 306

## Snippet Preview

```
// Package spec defines tau's built-in agent commands as declarative
// frontmatter + prompt-template files, modeled on GitHub Copilot's custom
// agent spec (name/description/tools/model/user-invocable) but scoped to
// what tau's coordinator can actually enforce today.
//
// This package is intentionally a leaf: it has no dependency on the agent
// coordinator or the command registry, so both can depend on it without
// creating an import cycle (the coordinator already depends on the registry).
package spec

import (
	"embed"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/samcharles93/tau/internal/skills"
)

//go:embed templates/*.agent.md
var templateFS embed.FS

// builtinFiles is the deterministic load and display order for built-in
// agent definitions.
var builtinFiles = []string{
	"tau.agent.md",
	"init.agent.md",
	"plan.agent.md",
```
