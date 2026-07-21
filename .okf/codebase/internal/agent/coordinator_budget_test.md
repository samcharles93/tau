---
description: Source module internal/agent/coordinator_budget_test.go (136 lines).
resource: internal/agent/coordinator_budget_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: coordinator_budget_test.go
type: Module
---

# Module coordinator_budget_test.go

**Path**: `internal/agent/coordinator_budget_test.go`  
**Lines**: 136

## Snippet Preview

```
package agent

import (
	"context"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/stretchr/testify/require"
)

// fakeStreamer returns text-only responses with no tool calls.
type fakeStreamer struct{ calls int }

func (f *fakeStreamer) StreamChatCompletionFull(_ context.Context, _ chat.ChatSessionState, _ string, _ map[string]string, _ chat.StreamCallbacks) (chat.CompletionResult, error) {
	f.calls++
	return chat.CompletionResult{
		FinishReason: "stop",
		Usage:        chat.ChatUsage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
	}, nil
}

// newBudgetCoordinator subscribes to the bus BEFORE constructing the
// coordinator - fakeStreamer completes synchronously with no real I/O, so
// the coordinator's own goroutine can process a submitted turn and publish
// ChatResponseCompletedEvent fast enough to race ahead of a subscription
// created afterward (the bus has no replay buffer - see
```
