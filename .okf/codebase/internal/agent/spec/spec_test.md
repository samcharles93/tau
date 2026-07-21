---
description: Source module internal/agent/spec/spec_test.go (189 lines).
resource: internal/agent/spec/spec_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: spec_test.go
type: Module
---

# Module spec_test.go

**Path**: `internal/agent/spec/spec_test.go`  
**Lines**: 189

## Snippet Preview

```
package spec

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuiltins_ParsesAllDefinitions(t *testing.T) {
	defs, err := Builtins()
	require.NoError(t, err)
	require.Len(t, defs, len(builtinFiles))

	names := make(map[string]*Definition, len(defs))
	for _, def := range defs {
		require.NotEmpty(t, def.Name)
		require.NotEmpty(t, def.Description)
		require.NotEmpty(t, def.Body)
		names[def.Name] = def
	}

	// task.agent.md was retired in P0.4 - tau is now the spawnable
	// general-purpose child worker. tau is not user-invocable.
	// Everything else defaults (or is explicitly set) to user-invocable.
	for _, def := range defs {
		if def.Name == "tau" {
			require.False(t, def.UserInvocable, "expected tau to not be user-invocable")
			require.False(t, def.ModeSwitcher, "expected tau to not be mode-switcher")
			continue
```
