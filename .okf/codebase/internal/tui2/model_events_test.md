---
description: Source module internal/tui2/model_events_test.go (993 lines).
resource: internal/tui2/model_events_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: model_events_test.go
type: Module
---

# Module model_events_test.go

**Path**: `internal/tui2/model_events_test.go`  
**Lines**: 993

## Snippet Preview

```
package tui2

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/tui/notify"
)

func TestHandleChatEventSnapshot(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	state := tauchat.ChatSessionState{
		SessionID:    "sess-1",
		Model:        tauchat.ChatModelRef{ID: "gpt-4"},
		ProviderName: "openai",
		Messages: []tauchat.ChatMessage{
			{Role: tauchat.ChatRoleUser, Content: "hello"},
			{Role: tauchat.ChatRoleAssistant, Content: "hi there"},
		},
	}

	m.handleChatEvent(tauchat.ChatSessionSnapshotEvent{State: state})

	if m.modelName != "gpt-4" {
		t.Fatalf("modelName = %q, want %q", m.modelName, "gpt-4")
	}
```
