---
description: Source module internal/agent/coordinator_agent_tools_test.go (596 lines).
resource: internal/agent/coordinator_agent_tools_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: coordinator_agent_tools_test.go
type: Module
---

# Module coordinator_agent_tools_test.go

**Path**: `internal/agent/coordinator_agent_tools_test.go`  
**Lines**: 596

## Snippet Preview

```
package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/stretchr/testify/require"
)

// writeAgentDefWithTools writes a minimal .agent.md file with an explicit
// tools list under dir/<name>.agent.md.
func writeAgentDefWithTools(t *testing.T, dir, name string, tools []string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	var toolsYAML strings.Builder
	for _, tl := range tools {
		toolsYAML.WriteString("\n  - ")
		toolsYAML.WriteString(tl)
	}
	content := "---\nname: " + name + "\ndescription: test agent\ntools:" + toolsYAML.String() + "\n---\n\nBody.\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".agent.md"), []byte(content), 0o644))
```
