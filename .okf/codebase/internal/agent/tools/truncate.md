---
description: Source module internal/agent/tools/truncate.go (147 lines).
resource: internal/agent/tools/truncate.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: truncate.go
type: Module
---

# Module truncate.go

**Path**: `internal/agent/tools/truncate.go`  
**Lines**: 147

## Snippet Preview

```
package tools

import (
	"fmt"
	"strings"
	"time"
)

const (
	// DefaultMaxBytes is the maximum byte size for tool output sent to the LLM.
	DefaultMaxBytes = 50 * 1024 // 50KB

	// DefaultMaxLines is the maximum line count for tool output sent to the LLM.
	DefaultMaxLines = 2000

	// DefaultToolTimeout is the per-tool execution deadline. Tools that exceed
	// this are cancelled. The shell tool uses its own configurable timeout.
	DefaultToolTimeout = 60 * time.Second
)

// TruncationResult holds the potentially truncated content and metadata.
type TruncationResult struct {
	Content      string
	Truncated    bool
	OriginalSize int
	OriginalLine int
	OutputLines  int // number of complete lines kept in Content
}

// TruncateHeadRaw keeps the first N lines/bytes, dropping the tail, without
```
