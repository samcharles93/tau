---
description: Source module internal/app/child_test.go (288 lines).
resource: internal/app/child_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: child_test.go
type: Module
---

# Module child_test.go

**Path**: `internal/app/child_test.go`  
**Lines**: 288

## Snippet Preview

```
package app

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/agent/stdio"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/store"
)

// TestRunChild_InvalidHandshake verifies the child exits non-zero when the
// first message on stdin is not agent.assign.
func TestRunChild_InvalidHandshake(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TAU_CONFIG_DIR", dir)

	childStdin, parentStdin, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	childStdout, _, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	origStdin := os.Stdin
```
