---
description: Source module internal/agent/tools/shell.go (197 lines).
resource: internal/agent/tools/shell.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: shell.go
type: Module
---

# Module shell.go

**Path**: `internal/agent/tools/shell.go`  
**Lines**: 197

## Snippet Preview

```
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const (
	defaultShellTimeout = 120 * time.Second
	maxShellTimeout     = 10 * time.Minute
)

// ShellParams are the parameters for the shell tool.
type ShellParams struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"` // seconds, defaults to 120
}

var shellSchema = Schema{
	Name:        "shell",
	Description: fmt.Sprintf("Execute a shell command. Uses PowerShell on Windows, bash on Linux/macOS. Returns stdout and stderr, truncated to the last %d lines or %s (whichever is hit first); when truncated, the full output is saved to a temp file whose path is included in the notice. Use for builds, tests, git, and other commands - prefer the dedicated grep, find, and read tools for searching and reading files.", DefaultMaxLines, FormatSize(DefaultMaxBytes)),
	Parameters: json.RawMessage(`{
```
