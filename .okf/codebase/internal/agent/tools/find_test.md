---
description: Source module internal/agent/tools/find_test.go (148 lines).
resource: internal/agent/tools/find_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: find_test.go
type: Module
---

# Module find_test.go

**Path**: `internal/agent/tools/find_test.go`  
**Lines**: 148

## Snippet Preview

```
package tools

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestFindFallback(t *testing.T) {
	// Create a temporary directory tree.
	tmp := t.TempDir()
	createTestFile(t, tmp, "foo.go", "package main")
	createTestFile(t, tmp, "bar.go", "package bar")
	createTestFile(t, tmp, "README.md", "# readme")
	createTestFile(t, tmp, "subdir/baz.go", "package baz")
	createTestFile(t, tmp, "subdir/nested/deep.txt", "deep")
	createTestFile(t, tmp, "vendor/mod.go", "package mod")
	os.MkdirAll(filepath.Join(tmp, "hiddendir"), 0o755)
	createTestFile(t, tmp, "hiddendir/visible.txt", "text")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cases := []struct {
		name      string
		params    FindParams
```
