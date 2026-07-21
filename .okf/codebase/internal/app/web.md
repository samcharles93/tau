---
description: Source module internal/app/web.go (78 lines).
resource: internal/app/web.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: web.go
type: Module
---

# Module web.go

**Path**: `internal/app/web.go`  
**Lines**: 78

## Snippet Preview

```
package app

import (
	"context"
	"fmt"
	"log/slog"

	webbridge "github.com/samcharles93/tau/internal/bridge"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	webserver "github.com/samcharles93/tau/internal/server"
	"github.com/samcharles93/tau/internal/spa"
)

// webServerResult holds the started web UI server and its reachable URL.
type webServerResult struct {
	Server   *webserver.Server
	URL      string
	Shutdown func()
	Wait     func()
}

// startWebUI starts the optional Web UI bridge and HTTP server. It returns the
// server result and the reachable URL, or empty values if the web UI is disabled
// or fails to start. The caller is responsible for calling Shutdown/Wait.
func startWebUI(
	ctx context.Context,
	runtime webbridge.Runtime,
	bus *eventbus.Bus,
	opts ChatOptions,
```
