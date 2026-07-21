---
description: Source module internal/agent/coordinator_cancel_test.go (202 lines).
resource: internal/agent/coordinator_cancel_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: coordinator_cancel_test.go
type: Module
---

# Module coordinator_cancel_test.go

**Path**: `internal/agent/coordinator_cancel_test.go`  
**Lines**: 202

## Snippet Preview

```
package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/stretchr/testify/require"
)

// blockingStreamer blocks the streaming call until the test releases it, so
// we can race a cancel against the in-flight turn.
type blockingStreamer struct {
	release chan struct{}
	called  chan struct{}
	once    sync.Once
}

func newBlockingStreamer() *blockingStreamer {
	return &blockingStreamer{
		release: make(chan struct{}),
		called:  make(chan struct{}),
	}
}

```
