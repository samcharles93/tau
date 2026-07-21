---
description: Source module internal/agent/tools/fsutil.go (37 lines).
resource: internal/agent/tools/fsutil.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: fsutil.go
type: Module
---

# Module fsutil.go

**Path**: `internal/agent/tools/fsutil.go`  
**Lines**: 37

## Snippet Preview

```
package tools

import (
	"os"
	"path/filepath"
)

// writeFileAtomic replaces path atomically using a temp file in the same directory.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = os.Remove(tmpPath)
	}
	defer cleanup()

	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
```
