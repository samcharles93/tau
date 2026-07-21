---
description: Source module internal/plugin/exec_unix.go (10 lines).
resource: internal/plugin/exec_unix.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: exec_unix.go
type: Module
---

# Module exec_unix.go

**Path**: `internal/plugin/exec_unix.go`  
**Lines**: 10

## Snippet Preview

```
//go:build !windows

package plugin

import "os"

// isExecutableByPlatform checks Unix execute permission bits.
func isExecutableByPlatform(_ string, info os.FileInfo) bool {
	return info.Mode()&0o111 != 0
}
```
