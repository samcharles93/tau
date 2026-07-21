---
description: Source module internal/cli/setup_test.go (99 lines).
resource: internal/cli/setup_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: setup_test.go
type: Module
---

# Module setup_test.go

**Path**: `internal/cli/setup_test.go`  
**Lines**: 99

## Snippet Preview

```
package cli

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samcharles93/tau/internal/app"
)

func TestSetupExitPlanSuccessWithModel(t *testing.T) {
	code, stdoutMsg, stderrMsg := setupExitPlan(app.SetupResult{ProviderID: "deepseek", ProviderName: "DeepSeek", Model: "deepseek-chat"}, nil)
	assert.Equal(t, 0, code)
	assert.Contains(t, stdoutMsg, "DeepSeek")
	assert.Contains(t, stdoutMsg, "deepseek-chat")
	assert.Empty(t, stderrMsg)
}

func TestSetupExitPlanSuccessWithoutModel(t *testing.T) {
	code, stdoutMsg, stderrMsg := setupExitPlan(app.SetupResult{ProviderID: "deepseek", ProviderName: "DeepSeek"}, nil)
	assert.Equal(t, 0, code)
	assert.Contains(t, stdoutMsg, "DeepSeek")
	assert.Contains(t, stdoutMsg, "/model")
	assert.Empty(t, stderrMsg)
}
```
