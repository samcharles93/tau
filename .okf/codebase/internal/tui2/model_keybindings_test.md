---
description: Source module internal/tui2/model_keybindings_test.go (606 lines).
resource: internal/tui2/model_keybindings_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: model_keybindings_test.go
type: Module
---

# Module model_keybindings_test.go

**Path**: `internal/tui2/model_keybindings_test.go`  
**Lines**: 606

## Snippet Preview

```
package tui2

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

func TestBashModeClearsRunningOnMatchingCompletion(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	cmd := m.handleBashCommand("ls")
	if !m.bashRunning {
		t.Fatal("expected bashRunning=true immediately after handleBashCommand")
	}
	if m.bashCallID == "" {
		t.Fatal("expected bashCallID to be populated so the completion event can be matched")
	}
	drainCmd(cmd) // perform the send

	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command sent, got %d", len(rt.sent))
	}
```
