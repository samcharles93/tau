---
description: Source module internal/metrics/tracker_test.go (403 lines).
resource: internal/metrics/tracker_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: tracker_test.go
type: Module
---

# Module tracker_test.go

**Path**: `internal/metrics/tracker_test.go`  
**Lines**: 403

## Snippet Preview

```
package metrics_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/metrics"
)

// newHarness wires a bus, a usage tracker (which activates emission by
// subscribing), and a publisher standing in for the coordinator.
func newHarness(t *testing.T) (*metrics.UsageTracker, *eventbus.Publisher[chat.MetricEvent]) {
	t.Helper()
	bus := eventbus.New()
	t.Cleanup(bus.Close)

	tracker := metrics.NewUsageTracker(bus.Client("usage"))
	t.Cleanup(tracker.Close)

	pub := eventbus.Publish[chat.MetricEvent](bus.Client("coordinator"))
	return tracker, pub
}

func llmResponse(session string, total float64, prompt, completion, provider, model string) chat.MetricEvent {
	return chat.MetricEvent{
		Category: chat.MetricCategoryLLM,
```
