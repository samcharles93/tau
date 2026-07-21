---
description: Source module internal/plugin/exec_windows.go (21 lines).
resource: internal/plugin/exec_windows.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: exec_windows.go
type: Module
---

# Module exec_windows.go

**Path**: `internal/plugin/exec_windows.go`  
**Lines**: 21

## Snippet Preview

```
//go:build windows

package plugin

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// windowsExecutableExts lists extensions that Windows treats as executable.
// Compiled Go plugins are the expected use case; .ps1 is omitted because
// PowerShell scripts require an interpreter and are not self-executable.
var windowsExecutableExts = []string{".exe", ".bat", ".cmd", ".com"}

// isExecutableByPlatform checks whether the file has a Windows-executable extension.
func isExecutableByPlatform(path string, _ os.FileInfo) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return slices.Contains(windowsExecutableExts, ext)
}
```
