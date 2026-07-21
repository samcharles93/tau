---
description: Source module internal/agent/tools/rg/rg_linux_amd64.go (10 lines).
resource: internal/agent/tools/rg/rg_linux_amd64.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: rg_linux_amd64.go
type: Module
---

# Module rg_linux_amd64.go

**Path**: `internal/agent/tools/rg/rg_linux_amd64.go`  
**Lines**: 10

## Snippet Preview

```
//go:build linux && amd64

package rg

import _ "embed"

const isWindows = false

//go:embed rg-linux-amd64
var binary []byte
```
