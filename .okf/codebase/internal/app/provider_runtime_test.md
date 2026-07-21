---
description: Source module internal/app/provider_runtime_test.go (68 lines).
resource: internal/app/provider_runtime_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: provider_runtime_test.go
type: Module
---

# Module provider_runtime_test.go

**Path**: `internal/app/provider_runtime_test.go`  
**Lines**: 68

## Snippet Preview

```
package app

import (
	"context"
	"strings"
	"testing"

	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/providers"
)

func TestProviderRuntimeReloadToEmpty(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("TAU_CONFIG_DIR", t.TempDir())
	for _, entry := range providers.Catalog() {
		for _, name := range entry.EnvVars {
			t.Setenv(name, "")
		}
	}
	state, err := providers.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range providers.Catalog() {
		if entry.Auth == providers.AuthNone {
			state.Disable(entry.ID)
		}
	}
	if err := state.Save(); err != nil {
```
