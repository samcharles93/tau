---
description: Source module internal/app/setup_test.go (453 lines).
resource: internal/app/setup_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: setup_test.go
type: Module
---

# Module setup_test.go

**Path**: `internal/app/setup_test.go`  
**Lines**: 453

## Snippet Preview

```
package app

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/providers"
)

// sandboxConfigDir points config.Dir()/providers.StatePath() at a fresh temp
// dir so RunSetup tests never touch the real user's config.yaml/auth.yaml.
func sandboxConfigDir(t *testing.T) {
	t.Helper()
	t.Setenv("TAU_CONFIG_DIR", t.TempDir())
}

type selectCall struct {
	title   string
	options []SetupOption
}

// fakeSetupPrompter is a scripted SetupPrompter for testing RunSetup without
// a real terminal. selectPick chooses among the options offered on each
// Select call; nil defaults to the first option.
type fakeSetupPrompter struct {
```
