---
description: Source module internal/agent/instantiate_test.go (200 lines).
resource: internal/agent/instantiate_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: instantiate_test.go
type: Module
---

# Module instantiate_test.go

**Path**: `internal/agent/instantiate_test.go`  
**Lines**: 200

## Snippet Preview

```
package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/procid"
	"github.com/samcharles93/tau/internal/store"
	"github.com/stretchr/testify/require"
)

func newSweepTestStore(t *testing.T) *store.SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	s, err := store.NewSQLiteStore(context.Background(), filepath.Join(dir, "test.db"), dir)
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

func saveOpenInstance(t *testing.T, s store.SessionStore, id string, pid int, processStartNS int64, startedAt time.Time) {
	t.Helper()
	require.NoError(t, s.SaveAgentInstance(context.Background(), store.AgentInstance{
		ID:               id,
		SpecName:         "tau",
		SpecScope:        "builtin",
```
