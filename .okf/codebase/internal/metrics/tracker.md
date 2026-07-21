---
description: Source module internal/metrics/tracker.go (151 lines).
resource: internal/metrics/tracker.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: tracker.go
type: Module
---

# Module tracker.go

**Path**: `internal/metrics/tracker.go`  
**Lines**: 151

## Snippet Preview

```
package metrics

import (
	"strconv"
	"sync"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
)

// SessionTotals holds aggregated metrics for one session.
type SessionTotals struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	// LastPromptTokens is the prompt-token count of the most recent llm.response
	// turn (overwritten each turn), used to compute context-window utilisation.
	LastPromptTokens int
	Cost             float64
	TurnCount        int
	ToolCalls        int
	ToolErrors       int
	LastProvider     string
	LastModel        string
	// TurnDurationMs accumulates turn.duration metric values (ms).
	// This is wall-clock time for the entire agentic turn including
	// all LLM calls and tool executions in the loop.
	TurnDurationMs int64
	// SkillActivations counts unique skill activation events.
	SkillActivations int
```
