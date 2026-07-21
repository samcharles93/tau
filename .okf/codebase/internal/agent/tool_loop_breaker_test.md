---
description: Source module internal/agent/tool_loop_breaker_test.go (285 lines).
resource: internal/agent/tool_loop_breaker_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: tool_loop_breaker_test.go
type: Module
---

# Module tool_loop_breaker_test.go

**Path**: `internal/agent/tool_loop_breaker_test.go`  
**Lines**: 285

## Snippet Preview

```
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/stretchr/testify/require"
)

// --- pure-function tests ----------------------------------------------------

func TestParseToolCallKeyIgnoresJustificationField(t *testing.T) {
	plain, jPlain := parseToolCallKey("grep", `{"pattern":"x","path":"/a"}`)
	justified, jJustified := parseToolCallKey("grep", `{"pattern":"x","path":"/a","repeat_justification":"polling for a state change"}`)
	if plain != justified {
		t.Fatalf("parseToolCallKey should ignore repeat_justification in key, got %q vs %q", plain, justified)
	}
	if jPlain != "" {
		t.Fatalf("expected empty justification, got %q", jPlain)
	}
	if jJustified != "polling for a state change" {
		t.Fatalf("expected justification 'polling for a state change', got %q", jJustified)
	}
```
