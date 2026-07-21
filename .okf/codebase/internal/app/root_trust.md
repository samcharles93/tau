---
description: Source module internal/app/root_trust.go (137 lines).
resource: internal/app/root_trust.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: root_trust.go
type: Module
---

# Module root_trust.go

**Path**: `internal/app/root_trust.go`  
**Lines**: 137

## Snippet Preview

```
package app

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/samcharles93/tau/internal/agent"
	"github.com/samcharles93/tau/internal/agent/spec"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/skills"
	"github.com/samcharles93/tau/internal/trust"
	"golang.org/x/term"
)

// rootSpecDisplay carries the resolved-root-spec facts the caller shows in
// the startup log and TUI status area, per docs/specs/agents/
// 01-agent-spec-format.md (Root-spec override trust: "Display before
// execution").
type rootSpecDisplay struct {
	// Scope is "builtin", "user", or "project".
	Scope string
	// Source is the file path a filesystem-loaded spec was parsed from;
	// empty for built-ins.
	Source string
	// Hash is the hex sha256 of Source's contents; empty for built-ins.
	Hash string
	// Trusted is true when Source's override is in effect (built-ins and
```
