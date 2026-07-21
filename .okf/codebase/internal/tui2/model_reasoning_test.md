---
description: Source module internal/tui2/model_reasoning_test.go (415 lines).
resource: internal/tui2/model_reasoning_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: model_reasoning_test.go
type: Module
---

# Module model_reasoning_test.go

**Path**: `internal/tui2/model_reasoning_test.go`  
**Lines**: 415

## Snippet Preview

```
package tui2

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
)

func TestHandleChatEventReasoningDelta(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.ChatReasoningDeltaEvent{Delta: "thinking..."})

	if m.reasoning != "thinking..." {
		t.Fatalf("reasoning = %q, want %q", m.reasoning, "thinking...")
	}
}

func TestToolCallCommitsReasoningAtContentBoundary(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.showReasoning = true
	m.reasoning = "reasoning before tool"

	m.handleChatEvent(tauchat.ChatToolCallDeltaEvent{
		CallID:   "tool-1",
```
