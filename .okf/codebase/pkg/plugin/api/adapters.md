---
description: Source module pkg/plugin/api/adapters.go (337 lines).
resource: pkg/plugin/api/adapters.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: adapters.go
type: Module
---

# Module adapters.go

**Path**: `pkg/plugin/api/adapters.go`  
**Lines**: 337

## Snippet Preview

```
package api

import (
	"context"

	"github.com/hashicorp/go-plugin"
	"github.com/samcharles93/tau/internal/chat"
)

// GRPCServer adapts an Extension implementation to the gRPC service.
type GRPCServer struct {
	UnimplementedExtensionServiceServer
	Impl   Extension
	broker *plugin.GRPCBroker
}

// Init receives the host's HostService broker id, dials it, and hands the
// resulting Host to the Extension if it is HostAware.
func (s *GRPCServer) Init(ctx context.Context, req *InitRequest) (*InitResponse, error) {
	if s.broker == nil || req.GetHostBrokerId() == 0 {
		return &InitResponse{}, nil
	}
	conn, err := s.broker.Dial(req.GetHostBrokerId())
	if err != nil {
		return nil, err
	}
	host := &hostClient{client: NewHostServiceClient(conn), pluginName: req.GetPluginName()}
	if aware, ok := s.Impl.(HostAware); ok {
		aware.SetHost(host)
	}
```
