---
description: Source module internal/providers/providers_test.go (391 lines).
resource: internal/providers/providers_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: providers_test.go
type: Module
---

# Module providers_test.go

**Path**: `internal/providers/providers_test.go`  
**Lines**: 391

## Snippet Preview

```
package providers

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samcharles93/tau/internal/config"
)

func fakeEnv(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func TestCatalogDetectEnvVar(t *testing.T) {
	entry, ok := Lookup("together")
	require.True(t, ok)

	// Falls through to the second candidate var.
	name, present := entry.DetectEnvVar(fakeEnv(map[string]string{"TOGETHERAI_API_KEY": "sk-x"}))
	assert.True(t, present)
```
