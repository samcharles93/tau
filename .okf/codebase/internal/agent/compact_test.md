---
description: Source module internal/agent/compact_test.go (91 lines).
resource: internal/agent/compact_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: compact_test.go
type: Module
---

# Module compact_test.go

**Path**: `internal/agent/compact_test.go`  
**Lines**: 91

## Snippet Preview

```
package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/stretchr/testify/require"
)

type compactTestStreamer struct {
	seen []chat.ChatSessionState
}

func (s *compactTestStreamer) StreamChatCompletionFull(
	_ context.Context,
	session chat.ChatSessionState,
	_ string,
	_ map[string]string,
	cb chat.StreamCallbacks,
) (chat.CompletionResult, error) {
	s.seen = append(s.seen, chat.CloneChatSessionState(&session))
	if cb.OnDelta != nil {
		_ = cb.OnDelta("summary of previous work")
	}
	return chat.CompletionResult{FinishReason: "stop"}, nil
```
