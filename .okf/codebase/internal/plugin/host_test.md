---
description: Source module internal/plugin/host_test.go (288 lines).
resource: internal/plugin/host_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: host_test.go
type: Module
---

# Module host_test.go

**Path**: `internal/plugin/host_test.go`  
**Lines**: 288

## Snippet Preview

```
package plugin

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/samcharles93/tau/pkg/plugin/api"
	"github.com/stretchr/testify/require"
)

func newTestHost(t *testing.T) *hostService {
	t.Helper()
	return &hostService{
		config: map[string]map[string]any{
			"mcp-plugin": {
				"servers": []any{map[string]any{"name": "fs"}},
				"enabled": true,
			},
		},
		kv: newKVStore(filepath.Join(t.TempDir(), "kv.json")),
	}
}

func TestHostServiceGetConfig(t *testing.T) {
	h := newTestHost(t)
```
