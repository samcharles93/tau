---
description: Source module internal/chat/commands/commands.go (189 lines).
resource: internal/chat/commands/commands.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: commands.go
type: Module
---

# Module commands.go

**Path**: `internal/chat/commands/commands.go`  
**Lines**: 189

## Snippet Preview

```
package commands

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/skills"
)

var namedArgPattern = regexp.MustCompile(`\$([A-Z][A-Z0-9_]*)`)

const (
	userCommandPrefix    = "user:"
	projectCommandPrefix = "project:"
)

// Argument represents a command argument with its metadata.
type Argument struct {
	ID          string
	Title       string
	Description string
	Required    bool
}

// CustomCommand represents a user-defined custom command loaded from markdown files.
type CustomCommand struct {
```
