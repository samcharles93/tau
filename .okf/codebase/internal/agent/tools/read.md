---
description: Source module internal/agent/tools/read.go (176 lines).
resource: internal/agent/tools/read.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: read.go
type: Module
---

# Module read.go

**Path**: `internal/agent/tools/read.go`  
**Lines**: 176

## Snippet Preview

```
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

// ReadParams are the parameters for the read tool.
type ReadParams struct {
	Path   string `json:"path"`
	File   string `json:"file,omitempty"`   // compatibility alias used by some providers
	Offset int    `json:"offset,omitempty"` // start line (1-based)
	Limit  int    `json:"limit,omitempty"`  // max lines to read
	Full   bool   `json:"full,omitempty"`   // explicitly allow a full-file response
}

// DefaultReadLines is the bounded line count used when no explicit read limit
// or full-file request is supplied.
const DefaultReadLines = 400

var readSchema = Schema{
	Name:        "read",
	Description: fmt.Sprintf("Read file contents. Omitted limits return at most %d lines; set full:true only when the complete file is genuinely needed. Output is always capped at %d lines or %s and includes a continuation offset.", DefaultReadLines, DefaultMaxLines, FormatSize(DefaultMaxBytes)),
	// NOTE: file is a compatibility alias for path; the executor
	// handles the fallback (file → path) and returns a clear error
	// when both are empty. We intentionally do NOT use anyOf here
```
