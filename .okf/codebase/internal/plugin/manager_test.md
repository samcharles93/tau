---
description: Source module internal/plugin/manager_test.go (360 lines).
resource: internal/plugin/manager_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: manager_test.go
type: Module
---

# Module manager_test.go

**Path**: `internal/plugin/manager_test.go`  
**Lines**: 360

## Snippet Preview

```
package plugin

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/pkg/plugin/api"
	"google.golang.org/grpc"
)

// mockExtensionServiceClient implements api.ExtensionServiceClient for tests.
type mockExtensionServiceClient struct {
	api.ExtensionServiceClient // embed for the methods we don't override

	dispatchEventFunc func(ctx context.Context, in *api.DispatchEventRequest, opts ...grpc.CallOption) (*api.DispatchEventResponse, error)
	executeToolFunc   func(ctx context.Context, in *api.ExecuteToolRequest, opts ...grpc.CallOption) (*api.ExecuteToolResponse, error)

	// dispatchCallCount tracks how many times DispatchEvent was called.
	dispatchCallCount atomic.Int64
}

func (m *mockExtensionServiceClient) DispatchEvent(ctx context.Context, in *api.DispatchEventRequest, opts ...grpc.CallOption) (*api.DispatchEventResponse, error) {
	m.dispatchCallCount.Add(1)
	if m.dispatchEventFunc != nil {
```
