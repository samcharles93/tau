---
description: Source module internal/app/live_models.go (185 lines).
resource: internal/app/live_models.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: live_models.go
type: Module
---

# Module live_models.go

**Path**: `internal/app/live_models.go`  
**Lines**: 185

## Snippet Preview

```
package app

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
)

// liveModelTimeout bounds the per-provider /models probe so an unreachable
// endpoint (e.g. a stopped local Ollama server) fails fast instead of stalling
// model discovery.
const liveModelTimeout = 4 * time.Second

// liveModelRefs lists a provider's models at runtime from its OpenAI-compatible
// /models endpoint, for providers whose model set is dynamic and not baked into
// the embedded snapshot (e.g. a local Ollama server). Models are returned in the
// order the endpoint reports them, tagged with the provider. The generic
// /models endpoint carries no capability data, so no tool-call filtering is
// applied here; for the "ollama" provider specifically, capabilities (e.g.
// "thinking") are additionally probed via Ollama's native /api/tags so
```
