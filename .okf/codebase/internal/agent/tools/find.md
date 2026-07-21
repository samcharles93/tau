---
description: Source module internal/agent/tools/find.go (349 lines).
resource: internal/agent/tools/find.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: find.go
type: Module
---

# Module find.go

**Path**: `internal/agent/tools/find.go`  
**Lines**: 349

## Snippet Preview

```
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

// FindParams are the parameters for the find tool.
type FindParams struct {
	Path     string `json:"path,omitempty"`      // directory to search in
	Pattern  string `json:"pattern,omitempty"`   // glob pattern for file names
	Type     string `json:"type,omitempty"`      // "file", "directory", or empty for both
	MaxDepth int    `json:"max_depth,omitempty"` // max directory depth (0 = unlimited)
	Exclude  string `json:"exclude,omitempty"`   // glob pattern to exclude (e.g. 'node_modules', '*.test.*')
}

var findSchema = Schema{
	Name:        "find",
	Description: fmt.Sprintf("Find files and directories by name pattern, or list a directory's contents. Respects .gitignore when fd is available. Returns a list of matching paths, truncated to %d results or %s (whichever is hit first). Omit pattern and set max_depth:1 to list a single directory. Use exclude to skip unwanted directories.", DefaultMaxLines, FormatSize(DefaultMaxBytes)),
	Parameters: json.RawMessage(`{
		"type": "object",
```
