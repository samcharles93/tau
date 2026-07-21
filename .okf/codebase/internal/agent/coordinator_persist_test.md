---
description: Source module internal/agent/coordinator_persist_test.go (259 lines).
resource: internal/agent/coordinator_persist_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: coordinator_persist_test.go
type: Module
---

# Module coordinator_persist_test.go

**Path**: `internal/agent/coordinator_persist_test.go`  
**Lines**: 259

## Snippet Preview

```
package agent

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/sessions"
	"github.com/samcharles93/tau/internal/store"
	"github.com/stretchr/testify/require"
)

// newTestSessionManager returns a Manager backed by a throwaway SQLite store
// under t.TempDir(), so persistence tests never touch the user's real
// session store.
func newTestSessionManager(t *testing.T) *sessions.Manager {
	t.Helper()
	dir := t.TempDir()
	rawStore, err := store.NewSQLiteStore(context.Background(), filepath.Join(dir, "sessions.db"), dir)
	require.NoError(t, err)
	mgr := sessions.NewManager(rawStore)
	t.Cleanup(func() { _ = mgr.Close() })
	return mgr
}

// startAndCloseTestSession starts a session and waits for its
```
