---
description: Source module internal/providers/manage_test.go (223 lines).
resource: internal/providers/manage_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: manage_test.go
type: Module
---

# Module manage_test.go

**Path**: `internal/providers/manage_test.go`  
**Lines**: 223

## Snippet Preview

```
package providers

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sandboxConfigDir points config.Dir() (and therefore StatePath()) at a fresh
// temp dir so Manage tests never touch the real user's auth.yaml.
func sandboxConfigDir(t *testing.T) {
	t.Helper()
	t.Setenv("TAU_CONFIG_DIR", t.TempDir())
}

func TestManageToggleEnablesThenDisables(t *testing.T) {
	sandboxConfigDir(t)
	m := NewManage(fakeEnv(nil)) // DEEPSEEK_API_KEY unset -> keyless-style enable

	enabled, warning, err := m.Toggle("deepseek")
	require.NoError(t, err)
	assert.True(t, enabled)
	assert.Equal(t, "DEEPSEEK_API_KEY is not set", warning)

	state, err := LoadState()
	require.NoError(t, err)
	assert.True(t, state.IsEnabled("deepseek"))
```
