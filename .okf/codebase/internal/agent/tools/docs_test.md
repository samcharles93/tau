---
description: Source module internal/agent/tools/docs_test.go (143 lines).
resource: internal/agent/tools/docs_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: docs_test.go
type: Module
---

# Module docs_test.go

**Path**: `internal/agent/tools/docs_test.go`  
**Lines**: 143

## Snippet Preview

```
package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/samcharles93/tau/internal/agent/tools"
)

func TestDocsTool(t *testing.T) {
	tool := tools.NewDocsTool(nil)
	ctx := context.Background()

	t.Run("search", func(t *testing.T) {
		params := json.RawMessage(`{"query": "provider"}`)
		res, err := tool.Execute(ctx, params, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsError {
			t.Fatalf("unexpected error in tool output: %s", res.Content)
		}
		if strings.Contains(res.Content, "No matches found") {
			t.Errorf("expected search results to contain matches, but got: %q", res.Content)
		}
		if !strings.Contains(strings.ToLower(res.Content), "provider") {
			t.Errorf("expected search result to contain 'provider', got: %q", res.Content)
		}
```
