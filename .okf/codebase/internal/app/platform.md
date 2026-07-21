---
description: Source module internal/app/platform.go (51 lines).
resource: internal/app/platform.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: platform.go
type: Module
---

# Module platform.go

**Path**: `internal/app/platform.go`  
**Lines**: 51

## Snippet Preview

```
package app

import (
	"context"

	"github.com/samcharles93/ai-sdk/runtime"

	"github.com/samcharles93/tau/internal/config"
)

// TokenOptions holds the parameters for resolving a provider bearer token.
type TokenOptions struct {
	Provider config.ProviderConfig
	Insecure bool
}

// ResolveToken resolves the bearer token for the configured provider
// using the ai-sdk runtime's built-in auth resolvers.
func ResolveToken(ctx context.Context, opts TokenOptions) (string, error) {
	rt := newRuntimeForProvider(opts.Provider, opts.Insecure)
	auth, err := rt.ResolveAuth(ctx, opts.Provider.Name)
	if err != nil {
		return "", err
	}
	return auth.Token, nil
}

type ModelsOptions struct {
	Provider config.ProviderConfig
	Insecure bool
```
