---
description: Source module internal/agent/tools/write_test.go (68 lines).
resource: internal/agent/tools/write_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: write_test.go
type: Module
---

# Module write_test.go

**Path**: `internal/agent/tools/write_test.go`  
**Lines**: 68

## Snippet Preview

```
package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteTool_PopulatesDiffDetails_NewFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "f.txt")

	tool := NewWriteTool(tmp, NewMutationQueue(), nil)
	params := `{"path": "f.txt", "content": "hello\n"}`
	res, err := tool.Execute(context.Background(), json.RawMessage(params), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", res.Content)
	}

	details, ok := res.Details.(DiffDetails)
	if !ok {
		t.Fatalf("expected Details to be a DiffDetails, got %T", res.Details)
	}
	if details.OldContent != "" {
		t.Fatalf("expected empty OldContent for a new file, got %q", details.OldContent)
```
