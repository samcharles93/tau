---
description: Source module internal/metrics/file.go (81 lines).
resource: internal/metrics/file.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: file.go
type: Module
---

# Module file.go

**Path**: `internal/metrics/file.go`  
**Lines**: 81

## Snippet Preview

```
// Package metrics provides subscribers for the chat.MetricEvent stream:
// a file-based JSONL exporter and a per-session usage tracker.
package metrics

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
)

// FileSubscriber appends MetricEvents as JSONL to a file. It acquires a
// per-event mutex to serialize writes, since the bus delivers events
// sequentially on a single goroutine for the subscribing client.
type FileSubscriber struct {
	sub    *eventbus.SubscriberFunc[chat.MetricEvent]
	f      *os.File
	mu     sync.Mutex
	closed bool
}

// NewFileSubscriber creates a subscriber that writes metrics to the given
// directory as a single metrics.jsonl file. The directory is created if
// it doesn't exist. Returns an error if the directory cannot be created
// or the file cannot be opened for appending.
```
