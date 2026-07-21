---
description: Source module internal/app/provider_runtime.go (59 lines).
resource: internal/app/provider_runtime.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: provider_runtime.go
type: Module
---

# Module provider_runtime.go

**Path**: `internal/app/provider_runtime.go`  
**Lines**: 59

## Snippet Preview

```
package app

import (
	"context"
	"sync"

	"github.com/samcharles93/ai-sdk/runtime"

	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/providers"
)

// providerRuntime holds the ai-sdk runtime together with the set of usable
// providers it was built from. It can be rebuilt live when provider state
// changes (e.g. after /provider), so both the dynamic streamer and the
// model refresher always observe the current provider set without a restart.
type providerRuntime struct {
	insecure bool

	mu        sync.RWMutex
	rt        *runtime.Runtime
	providers []tauconfig.ProviderConfig
}

// newProviderRuntime wraps an already-built runtime and its provider set.
func newProviderRuntime(rt *runtime.Runtime, provs []tauconfig.ProviderConfig, insecure bool) *providerRuntime {
	return &providerRuntime{rt: rt, providers: provs, insecure: insecure}
}

// snapshot returns the current runtime and provider set together, taken under
```
