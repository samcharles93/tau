---
description: Source module internal/providers/effective.go (50 lines).
resource: internal/providers/effective.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: effective.go
type: Module
---

# Module effective.go

**Path**: `internal/providers/effective.go`  
**Lines**: 50

## Snippet Preview

```
package providers

import (
	"context"

	"github.com/samcharles93/tau/internal/config"
)

// Effective loads the user's configuration and managed provider state, then
// resolves them (together with the live environment) into a config.Config whose
// Providers are the set tau can actually use right now. The resolved slice is
// returned alongside for richer display (sources, availability) by callers such
// as the /providers command.
//
// Unlike config.LoadConfig, a missing config file and zero hand-written
// providers are not errors: env-detected and OAuth providers stand in.
func Effective(ctx context.Context) (config.Config, []ResolvedProvider, error) {
	return effective(ctx, nil)
}

func effective(ctx context.Context, getenv func(string) string) (config.Config, []ResolvedProvider, error) {
	cfg, err := config.LoadConfigAllowEmpty()
	if err != nil {
		return config.Config{}, nil, err
	}
	state, err := LoadState()
	if err != nil {
		return config.Config{}, nil, err
	}
	resolved, _ := ResolveWithRefresh(ctx, cfg, state, getenv)
```
