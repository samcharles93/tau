---
description: Source module internal/agent/coordinator_lifecycle_test.go (859 lines).
resource: internal/agent/coordinator_lifecycle_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: coordinator_lifecycle_test.go
type: Module
---

# Module coordinator_lifecycle_test.go

**Path**: `internal/agent/coordinator_lifecycle_test.go`  
**Lines**: 859

## Snippet Preview

```
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/pkg/plugin/api"
	"github.com/stretchr/testify/require"
)

// newTestBus returns a Bus that is automatically closed when the test completes.
func newTestBus(t *testing.T) *eventbus.Bus {
	t.Helper()
	bus := eventbus.New()
	t.Cleanup(bus.Close)
	return bus
}

type noopStreamer struct{}

func (noopStreamer) StreamChatCompletionFull(
```
