---
description: Source module internal/tui2/model_render_test.go (240 lines).
resource: internal/tui2/model_render_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: model_render_test.go
type: Module
---

# Module model_render_test.go

**Path**: `internal/tui2/model_render_test.go`  
**Lines**: 240

## Snippet Preview

```
package tui2

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

func TestCopyCommandReturnsRawContentAfterResponse(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	m.handleChatEvent(tauchat.ChatResponseDeltaEvent{Delta: "hello world"})
	m.handleChatEvent(tauchat.ChatResponseCompletedEvent{})

	if m.lastAssistantText != "hello world" {
		t.Fatalf("lastAssistantText = %q, want %q", m.lastAssistantText, "hello world")
	}

	cmd := m.cmdCopy("")
	if cmd == nil {
		t.Fatal("expected a non-nil Cmd once a response has completed")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) == 0 {
```
