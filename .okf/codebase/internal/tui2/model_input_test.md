---
description: Source module internal/tui2/model_input_test.go (952 lines).
resource: internal/tui2/model_input_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: model_input_test.go
type: Module
---

# Module model_input_test.go

**Path**: `internal/tui2/model_input_test.go`  
**Lines**: 952

## Snippet Preview

```
package tui2

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

// TestStreamCursorAppearsWhileStreaming checks the cursor shows up on the
// live view while text is actively streaming, and only there.
func TestStreamCursorAppearsWhileStreaming(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.agentState = agentThinking
	m.streaming = "the response so far"

	view := m.viewportLinesForView(false)
	joined := strings.Join(view, "\n")
	if !strings.Contains(joined, streamCursor) {
		t.Fatalf("expected the cursor while streaming, got %q", stripANSI(joined))
	}
}

// TestStreamCursorAbsentBeforeStreamingStarts checks the working indicator
// state (in response, nothing streamed yet) shows no cursor - there's no
```
