---
description: Source module internal/indexing/codesearch_test.go (249 lines).
resource: internal/indexing/codesearch_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: codesearch_test.go
type: Module
---

# Module codesearch_test.go

**Path**: `internal/indexing/codesearch_test.go`  
**Lines**: 249

## Snippet Preview

```
package indexing

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestBuildIndexAndCandidates(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "a.txt", "alpha needle omega\n")
	writeTestFile(t, root, "b.txt", "nothing here\n")
	indexPath := filepath.Join(t.TempDir(), "workspace.csearch")

	if err := BuildIndex(context.Background(), root, indexPath); err != nil {
		t.Fatalf("BuildIndex() error = %v", err)
	}
	files, err := IndexCandidates(indexPath, "needle", false, false)
	if err != nil {
		t.Fatalf("IndexCandidates() error = %v", err)
	}
	if len(files) != 1 || files[0] != filepath.Join(root, "a.txt") {
		t.Fatalf("candidates = %v", files)
	}
}

```
