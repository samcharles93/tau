---
description: Source module internal/sessions/manager_test.go (92 lines).
resource: internal/sessions/manager_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: manager_test.go
type: Module
---

# Module manager_test.go

**Path**: `internal/sessions/manager_test.go`  
**Lines**: 92

## Snippet Preview

```
package sessions

import (
	"context"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/store"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	s, err := store.NewSQLiteStore(context.Background(), dir+"/sessions.db", dir)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return NewManager(s)
}

// TestManagerListCarriesLineageAndAttribution verifies that List() maps
// parent_session_id and agent_instance_id (and the previously-dropped
// tool_calls/tool_errors) from the store row through to the chat-level
// SessionSummary - this is what the TUI/WebUI session tree is built from.
func TestManagerListCarriesLineageAndAttribution(t *testing.T) {
	m := newTestManager(t)
	ctx := context.Background()

```
