---
description: Source module internal/agent/tools/read_test.go (122 lines).
resource: internal/agent/tools/read_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: read_test.go
type: Module
---

# Module read_test.go

**Path**: `internal/agent/tools/read_test.go`  
**Lines**: 122

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
)

func execRead(t *testing.T, cwd string, params string) Result {
	t.Helper()
	tool := NewReadTool(cwd, nil)
	res, err := tool.Execute(context.Background(), json.RawMessage(params), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return res
}

func TestReadTool_RawContentNoGutter(t *testing.T) {
	tmp := t.TempDir()
	writeReadTestFile(t, tmp, "f.txt", "alpha\nbeta\ngamma\n")

	res := execRead(t, tmp, `{"path": "f.txt"}`)
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", res.Content)
	}
```
