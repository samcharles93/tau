---
description: Source module internal/app/provider.go (160 lines).
resource: internal/app/provider.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: provider.go
type: Module
---

# Module provider.go

**Path**: `internal/app/provider.go`  
**Lines**: 160

## Snippet Preview

```
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/providers"
)

// ProviderStatus is one provider-list row prepared for presentation by a CLI
// or other frontend.
type ProviderStatus struct {
	ID      string
	Status  string
	Source  string
	Auth    string
	Details string
}

// ProviderLoginOption is one provider that supports an interactive login.
type ProviderLoginOption struct {
	ID    string
	Label string
}

```
