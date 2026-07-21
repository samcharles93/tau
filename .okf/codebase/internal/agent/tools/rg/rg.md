---
description: Source module internal/agent/tools/rg/rg.go (45 lines).
resource: internal/agent/tools/rg/rg.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: rg.go
type: Module
---

# Module rg.go

**Path**: `internal/agent/tools/rg/rg.go`  
**Lines**: 45

## Snippet Preview

```
//go:build (darwin && amd64) || (linux && amd64) || (windows && amd64)

// Package rg embeds a statically-linked ripgrep binary and exposes a single
// entry point so the grep tool can always use authoritative rg matching.
package rg

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var (
	extractOnce sync.Once
	extractPath string
	extractErr  error
)

// Path returns the filesystem path to the embedded rg binary, extracting it
// to a temporary file on first call. The caller must not remove the file.
func Path() (string, error) {
	extractOnce.Do(func() {
		extractPath, extractErr = extract()
	})
	if extractErr != nil {
		return "", extractErr
	}
	return extractPath, nil
}
```
