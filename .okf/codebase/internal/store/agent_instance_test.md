---
description: Source module internal/store/agent_instance_test.go (205 lines).
resource: internal/store/agent_instance_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: agent_instance_test.go
type: Module
---

# Module agent_instance_test.go

**Path**: `internal/store/agent_instance_test.go`  
**Lines**: 205

## Snippet Preview

```
package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/store"
	"github.com/stretchr/testify/require"
)

func TestSQLiteStore_SaveAndGetAgentInstance(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	inst := store.AgentInstance{
		ID:               "research#aaaaaa",
		SpecName:         "research",
		SpecScope:        "builtin",
		SpecHash:         "abc123",
		SpecSnapshot:     `{"name":"research"}`,
		ResolvedProvider: "openai",
		ResolvedModel:    "gpt-5",
		Depth:            1,
		StartedAt:        time.Now().UTC().Truncate(time.Second),
	}
	require.NoError(t, s.SaveAgentInstance(ctx, inst))

```
