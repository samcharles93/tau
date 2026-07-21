---
description: Source module internal/tui2/sessiontree_test.go (116 lines).
resource: internal/tui2/sessiontree_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: sessiontree_test.go
type: Module
---

# Module sessiontree_test.go

**Path**: `internal/tui2/sessiontree_test.go`  
**Lines**: 116

## Snippet Preview

```
package tui2

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

func TestCtrlOFetchesWhenCacheEmpty(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	cmd := m.dispatchKey(key('o', tea.ModCtrl))
	drainCmd(cmd)

	if m.sessionTreeOverlay == nil {
		t.Fatal("expected Ctrl+O to open m.sessionTreeOverlay")
	}
	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 command sent, got %d", len(rt.sent))
	}
	sent, ok := rt.sent[0].(tauchat.ListSessionsCommand)
	if !ok {
		t.Fatalf("expected ListSessionsCommand, got %T", rt.sent[0])
	}
	if !sent.Silent {
		t.Fatal("expected the prefetch to be silent")
```
