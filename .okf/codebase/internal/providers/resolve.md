---
description: Source module internal/providers/resolve.go (347 lines).
resource: internal/providers/resolve.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: resolve.go
type: Module
---

# Module resolve.go

**Path**: `internal/providers/resolve.go`  
**Lines**: 347

## Snippet Preview

```
package providers

import (
	"context"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/samcharles93/tau/internal/config"
)

// Source records where a resolved provider came from, for display and so the
// UI can explain why a provider is (or isn't) available.
type Source string

const (
	// SourceConfig is a provider declared in the user's hand-written config.
	SourceConfig Source = "config"
	// SourceEnv is a catalog provider enabled by the user and backed by an
	// environment variable.
	SourceEnv Source = "env"
	// SourceOAuth is a catalog provider authenticated through a login flow.
	SourceOAuth Source = "oauth"
	// SourceManaged is a catalog API-key provider backed by a key stored via
	// the setup wizard or `tau provider login`, used when no env var is
	// active for that provider.
	SourceManaged Source = "managed"
)

```
