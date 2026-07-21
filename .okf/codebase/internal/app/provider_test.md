---
description: Source module internal/app/provider_test.go (136 lines).
resource: internal/app/provider_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: provider_test.go
type: Module
---

# Module provider_test.go

**Path**: `internal/app/provider_test.go`  
**Lines**: 136

## Snippet Preview

```
package app

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/providers"
)

func TestListProvidersKeepsConfigAndManagedSourcesDistinct(t *testing.T) {
	sandboxConfigDir(t)
	require.NoError(t, config.SaveDefaultProviderAndModel("", "", ""))
	configPath := config.GlobalPath()
	require.NoError(t, os.WriteFile(configPath, []byte(`providers:
  - name: mistral
    base_url: https://mistral.example
    auth:
      type: none
`), 0o600))
	state, err := providers.LoadState()
	require.NoError(t, err)
	state.SetAPIKey("deepseek", "sk-managed")
	require.NoError(t, state.Save())
```
