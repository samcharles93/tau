---
description: Source module internal/agent/coordinator_persist_crud_test.go (468 lines).
resource: internal/agent/coordinator_persist_crud_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: coordinator_persist_crud_test.go
type: Module
---

# Module coordinator_persist_crud_test.go

**Path**: `internal/agent/coordinator_persist_crud_test.go`  
**Lines**: 468

## Snippet Preview

```
package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/sessions"
	"github.com/stretchr/testify/require"
)

// newTestCoordinatorWithManager creates a coordinator wired to the given
// session manager (nil is valid - it exercises the "persistence not
// available" paths). Unlike startAndCloseTestSession's throwaway
// coordinators, this one is left running so a test can issue several
// commands (list/load/delete/export) against a store populated earlier in
// the same test.
func newTestCoordinatorWithManager(t *testing.T, mgr *sessions.Manager) *Coordinator {
	t.Helper()
	bus := newTestBus(t)
	coordinator, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus: bus,
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
```
