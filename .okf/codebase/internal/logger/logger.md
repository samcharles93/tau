---
description: Source module internal/logger/logger.go (66 lines).
resource: internal/logger/logger.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: logger.go
type: Module
---

# Module logger.go

**Path**: `internal/logger/logger.go`  
**Lines**: 66

## Snippet Preview

```
package logger

import (
	"io"
	"log/slog"
)

// Format selects the slog handler encoding.
type Format string

const (
	FormatText Format = "text"
	FormatJSON Format = "json"
)

// Options controls how a logger writes to the provided destination.
type Options struct {
	Level     slog.Leveler
	AddSource bool
	Format    Format
}

// New builds a slog.Logger that writes to any io.Writer.
// A nil writer falls back to io.Discard.
func New(writer io.Writer, opts Options) *slog.Logger {
	if writer == nil {
		writer = io.Discard
	}

	handlerOpts := &slog.HandlerOptions{
```
