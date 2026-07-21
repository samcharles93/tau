---
description: Source module internal/server/server.go (152 lines).
resource: internal/server/server.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: server.go
type: Module
---

# Module server.go

**Path**: `internal/server/server.go`  
**Lines**: 152

## Snippet Preview

```
// Package server provides the HTTP and WebSocket surface for the Tau Web UI.
// It serves the embedded SPA and upgrades /ws to the bridge.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Bridge is the subset of internal/bridge.Bridge that the server needs.
type Bridge interface {
	UpgradeHTTP(w http.ResponseWriter, r *http.Request) error
	ClientCount() int
	Close() error
}

// Server serves the embedded SPA and the /ws WebSocket endpoint.
type Server struct {
	addr    string
	bridge  Bridge
	spa     http.Handler
	logger  *slog.Logger
```
