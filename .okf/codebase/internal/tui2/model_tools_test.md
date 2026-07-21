---
description: Source module internal/tui2/model_tools_test.go (1205 lines).
resource: internal/tui2/model_tools_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: model_tools_test.go
type: Module
---

# Module model_tools_test.go

**Path**: `internal/tui2/model_tools_test.go`  
**Lines**: 1205

## Snippet Preview

```
package tui2

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

func TestBashModeIgnoresUnrelatedToolCompletion(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	drainCmd(m.handleBashCommand("ls"))

	// An LLM tool call finishing must not be mistaken for the bash command.
	m.handleChatEvent(tauchat.ChatToolExecutionCompletedEvent{CallID: "unrelated-tool-call"})

	if !m.bashRunning {
		t.Error("an unrelated tool completion must not clear bashRunning")
	}
	if m.bashCallID == "" {
		t.Error("an unrelated tool completion must not clear bashCallID")
	}
}
```
