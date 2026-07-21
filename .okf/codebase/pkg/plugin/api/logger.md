---
description: Source module pkg/plugin/api/logger.go (63 lines).
resource: pkg/plugin/api/logger.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: logger.go
type: Module
---

# Module logger.go

**Path**: `pkg/plugin/api/logger.go`  
**Lines**: 63

## Snippet Preview

```
package api

import (
	"context"
	"fmt"
)

// PluginLogger is a helper for plugins to emit logs back to the host via gRPC.
// Plugins can call this to send structured logs to tau's main logger.
type PluginLogger struct {
	client ExtensionServiceClient
}

// NewPluginLogger creates a logger for use within a plugin.
// It requires the gRPC client to communicate back to the host.
func NewPluginLogger(client ExtensionServiceClient) *PluginLogger {
	return &PluginLogger{client: client}
}

// Debug logs at debug level.
func (pl *PluginLogger) Debug(ctx context.Context, msg string, fields map[string]string) error {
	return pl.log(ctx, "debug", msg, fields)
}

// Info logs at info level.
func (pl *PluginLogger) Info(ctx context.Context, msg string, fields map[string]string) error {
	return pl.log(ctx, "info", msg, fields)
}

// Warn logs at warn level.
```
