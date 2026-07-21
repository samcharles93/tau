---
description: Source module internal/agent/tools/rg/rg_windows_amd64.go (10 lines).
resource: internal/agent/tools/rg/rg_windows_amd64.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: rg_windows_amd64.go
type: Module
---

# Module rg_windows_amd64.go

**Path**: `internal/agent/tools/rg/rg_windows_amd64.go`  
**Lines**: 10

## Snippet Preview

```
//go:build windows && amd64

package rg

import _ "embed"

const isWindows = true

//go:embed rg-windows-amd64.exe
var binary []byte
```
