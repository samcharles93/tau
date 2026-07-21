---
description: Source module internal/logger/logger_test.go (53 lines).
resource: internal/logger/logger_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: logger_test.go
type: Module
---

# Module logger_test.go

**Path**: `internal/logger/logger_test.go`  
**Lines**: 53

## Snippet Preview

```
package logger

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestNewTextWritesToProvidedWriter(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := NewText(&buf, slog.LevelDebug)
	logger.Debug("hello", "target", "buffer")

	output := buf.String()
	if !strings.Contains(output, "level=DEBUG") {
		t.Fatalf("expected debug level in output, got %q", output)
	}
	if !strings.Contains(output, "msg=hello") {
		t.Fatalf("expected message in output, got %q", output)
	}
	if !strings.Contains(output, "target=buffer") {
		t.Fatalf("expected attributes in output, got %q", output)
	}
}

func TestNewJSONWritesStructuredOutput(t *testing.T) {
	t.Parallel()
```
