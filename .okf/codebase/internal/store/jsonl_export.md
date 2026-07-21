---
description: Source module internal/store/jsonl_export.go (59 lines).
resource: internal/store/jsonl_export.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: jsonl_export.go
type: Module
---

# Module jsonl_export.go

**Path**: `internal/store/jsonl_export.go`  
**Lines**: 59

## Snippet Preview

```
package store

import (
	"context"
	"fmt"
	"os"
)

// ExportSessionAsJSONL writes a session's messages to a JSONL file at
// outputPath. The write is atomic - data is written to a temp file first,
// then renamed. The app never reads JSONL files; this is a pure export.
func ExportSessionAsJSONL(ctx context.Context, store SessionStore, id, outputPath string) error {
	ch, errCh := store.ExportMessages(ctx, id)

	tmpPath := outputPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("store: create temp jsonl: %w", err)
	}

	var writeErr error
	for line := range ch {
		if _, err := f.Write(line); err != nil {
			writeErr = err
			break
		}
	}
	closeErr := f.Close()

	// Drain remaining messages so the export goroutine can exit cleanly.
```
