---
description: Source module internal/agent/coordinator_bash_test.go (268 lines).
resource: internal/agent/coordinator_bash_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: coordinator_bash_test.go
type: Module
---

# Module coordinator_bash_test.go

**Path**: `internal/agent/coordinator_bash_test.go`  
**Lines**: 268

## Snippet Preview

```
package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/stretchr/testify/require"
)

// fakeShellTool is a deterministic stand-in for the real "shell" tool
// (internal/agent/tools/shell.go), avoiding a dependency on an actual shell
// being available in the test environment. If release is non-nil, Execute
// blocks until release is closed or ctx is cancelled, so cancellation can be
// exercised; started (if non-nil) is closed once Execute has begun.
func fakeShellTool(started chan<- struct{}, release <-chan struct{}) tools.Tool {
	return tools.Tool{
		Schema: tools.Schema{Name: "shell", Description: "fake shell for tests"},
		Execute: func(ctx context.Context, params json.RawMessage, bridge tools.UIBridge) (tools.Result, error) {
			var p tools.ShellParams
			_ = json.Unmarshal(params, &p)
			bridge.Log("output for: " + p.Command)
			if started != nil {
				close(started)
			}
```
