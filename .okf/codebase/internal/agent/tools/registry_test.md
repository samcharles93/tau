---
description: Source module internal/agent/tools/registry_test.go (189 lines).
resource: internal/agent/tools/registry_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: registry_test.go
type: Module
---

# Module registry_test.go

**Path**: `internal/agent/tools/registry_test.go`  
**Lines**: 189

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

func dummyExecutor(_ context.Context, _ json.RawMessage, _ tools.UIBridge) (tools.Result, error) {
	return tools.Result{Content: "ok"}, nil
}

func TestRegistry_Register(t *testing.T) {
	r := tools.NewRegistry()

	tool := tools.Tool{
		Schema:  tools.Schema{Name: "test", Description: "a test tool"},
		Execute: dummyExecutor,
		Source:  "builtin",
	}

	if err := r.Register(tool); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if r.Count() != 1 {
		t.Fatalf("expected 1 tool, got %d", r.Count())
```
