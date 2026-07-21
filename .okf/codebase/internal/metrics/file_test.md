---
description: Source module internal/metrics/file_test.go (179 lines).
resource: internal/metrics/file_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: file_test.go
type: Module
---

# Module file_test.go

**Path**: `internal/metrics/file_test.go`  
**Lines**: 179

## Snippet Preview

```
package metrics_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/metrics"
)

func TestFileSubscriber_WritesJSONL(t *testing.T) {
	dir := t.TempDir()
	bus := eventbus.New()
	t.Cleanup(bus.Close)

	fs, err := metrics.NewFileSubscriber(bus.Client("file"), dir)
	require.NoError(t, err)
	defer fs.Close()

	pub := eventbus.Publish[chat.MetricEvent](bus.Client("pub"))
	pub.Publish(chat.MetricEvent{
		Category:  chat.MetricCategoryLLM,
		Name:      "llm.response",
```
