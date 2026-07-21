---
description: Source module internal/tui2/model_overlays_test.go (904 lines).
resource: internal/tui2/model_overlays_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: model_overlays_test.go
type: Module
---

# Module model_overlays_test.go

**Path**: `internal/tui2/model_overlays_test.go`  
**Lines**: 904

## Snippet Preview

```
package tui2

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

func TestConfirmPromptEnterUsesHighlightedOption(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	evt := tauchat.InteractivePromptRequestedEvent{
		RequestID: "req-1", Kind: "confirm", Title: "Delete?", Message: "sure?",
	}
	drainCmd(m.handleChatEvent(evt))

	if m.activePrompt == nil {
		t.Fatal("expected activePrompt to be set")
	}
	if !m.activePrompt.confirmYes {
		t.Fatal("expected the default highlighted option to be Yes")
	}

	// Toggle to "No" before submitting.
	m.handlePromptKey(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.activePrompt.confirmYes {
```
