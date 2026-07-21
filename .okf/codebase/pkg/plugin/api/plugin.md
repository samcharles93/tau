---
description: Source module pkg/plugin/api/plugin.go (103 lines).
resource: pkg/plugin/api/plugin.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: plugin.go
type: Module
---

# Module plugin.go

**Path**: `pkg/plugin/api/plugin.go`  
**Lines**: 103

## Snippet Preview

```
// Package api provides the shared types, interfaces, and gRPC adapters
// for tau's plugin system. Both the tau host and plugin binaries compile
// against this package.
package api

import (
	"context"

	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
)

// Handshake is the go-plugin handshake shared by the tau host and every
// plugin binary. ProtocolVersion gates wire compatibility: bump it whenever
// the proto contract changes incompatibly (package rename, field type change,
// enum renumbering) so stale plugin binaries fail cleanly at handshake
// instead of misbehaving mid-call.
//
// Version history:
//
//	1: original contract (proto package "proto")
//	2: proto package tau.plugin.v1; StackWidget.Direction/StatusWidget.State
//	   values renamed and State renumbered; dead fields removed
var Handshake = plugin.HandshakeConfig{
	ProtocolVersion:  2,
	MagicCookieKey:   "TAU_PLUGIN",
	MagicCookieValue: "tau",
}

// Capability identifiers a plugin may advertise via GetCapabilities. The set is
```
