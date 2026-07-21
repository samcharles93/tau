---
description: Source module internal/agent/tools/rg/rg_unsupported.go (13 lines).
resource: internal/agent/tools/rg/rg_unsupported.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: rg_unsupported.go
type: Module
---

# Module rg_unsupported.go

**Path**: `internal/agent/tools/rg/rg_unsupported.go`  
**Lines**: 13

## Snippet Preview

```
//go:build !((darwin && amd64) || (linux && amd64) || (windows && amd64))

// Package rg embeds a statically-linked ripgrep binary and exposes a single
// entry point so the grep tool can always use authoritative rg matching.
package rg

import "errors"

// Path reports that no ripgrep binary is bundled for this platform/arch.
// Callers fall back to the pure-Go grep implementation.
func Path() (string, error) {
	return "", errors.New("no embedded ripgrep binary for this platform")
}
```
