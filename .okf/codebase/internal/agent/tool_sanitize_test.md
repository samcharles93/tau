---
description: Source module internal/agent/tool_sanitize_test.go (213 lines).
resource: internal/agent/tool_sanitize_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: tool_sanitize_test.go
type: Module
---

# Module tool_sanitize_test.go

**Path**: `internal/agent/tool_sanitize_test.go`  
**Lines**: 213

## Snippet Preview

```
package agent

import (
	"testing"

	"github.com/samcharles93/tau/internal/chat"
)

func TestSanitizeToolCallArguments(t *testing.T) {
	tests := []struct {
		name     string
		calls    []chat.ChatToolCall
		wantN    int
		wantArgs []string
	}{
		{
			name:  "empty calls",
			calls: nil,
			wantN: 0,
		},
		{
			name: "valid JSON",
			calls: []chat.ChatToolCall{
				{
					ID: "call_1",
					Function: chat.ChatFunctionCall{
						Name:      "bash",
						Arguments: `{"command":"git log"}`,
					},
				},
```
