---
description: Source module internal/agent/tools/truncate_test.go (109 lines).
resource: internal/agent/tools/truncate_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: truncate_test.go
type: Module
---

# Module truncate_test.go

**Path**: `internal/agent/tools/truncate_test.go`  
**Lines**: 109

## Snippet Preview

```
package tools_test

import (
	"strings"
	"testing"

	"github.com/samcharles93/tau/internal/agent/tools"
)

func TestTruncateHead_NoTruncation(t *testing.T) {
	content := "line1\nline2\nline3"
	result := tools.TruncateHead(content, 100, 100000)

	if result.Truncated {
		t.Fatal("should not be truncated")
	}
	if result.Content != content {
		t.Fatalf("content mismatch: got %q", result.Content)
	}
}

func TestTruncateHead_ByLines(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "x"
	}
	content := strings.Join(lines, "\n")

	result := tools.TruncateHead(content, 10, 100000)

```
