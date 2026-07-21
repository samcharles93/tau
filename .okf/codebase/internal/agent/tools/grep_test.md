---
description: Source module internal/agent/tools/grep_test.go (299 lines).
resource: internal/agent/tools/grep_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: grep_test.go
type: Module
---

# Module grep_test.go

**Path**: `internal/agent/tools/grep_test.go`  
**Lines**: 299

## Snippet Preview

```
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type staticGrepIndex struct{ files []string }

func (s staticGrepIndex) Candidates(context.Context, string, bool, bool) ([]string, bool) {
	return s.files, true
}

func TestGrepUsesIndexCandidatesAsAdvisoryFileSet(t *testing.T) {
	tmp := t.TempDir()
	indexed := filepath.Join(tmp, "indexed.txt")
	createGrepTestFile(t, tmp, "indexed.txt", "needle\n")
	createGrepTestFile(t, tmp, "excluded.txt", "needle\n")
	tool := NewGrepTool(tmp, staticGrepIndex{files: []string{indexed}})
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"pattern":"needle"}`), nil)
	if err != nil || result.IsError {
		t.Fatalf("grep result = %#v, err = %v", result, err)
	}
	if !strings.Contains(result.Content, "indexed.txt") || strings.Contains(result.Content, "excluded.txt") {
```
