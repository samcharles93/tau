---
description: Source module internal/registry/agent_discovery_test.go (98 lines).
resource: internal/registry/agent_discovery_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: agent_discovery_test.go
type: Module
---

# Module agent_discovery_test.go

**Path**: `internal/registry/agent_discovery_test.go`  
**Lines**: 98

## Snippet Preview

```
package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/samcharles93/tau/internal/eventbus"
)

func writeAgentFile(t *testing.T, dir, name, description string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writing agent fixture: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, name+".agent.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("writing agent fixture: %v", err)
	}
}

// TestDiscoveredAgentAppearsWithProjectPrefix guards Part A of the
// agent-authored-subagent feature: a definition dropped under
// <cwd>/.agents/agents/ must show up in the registry with the project:
// prefix (execution is wired separately - see agentCommands' doc comment).
func TestDiscoveredAgentAppearsWithProjectPrefix(t *testing.T) {
	cwd := t.TempDir()
	writeAgentFile(t, filepath.Join(cwd, ".agents", "agents"), "reviewer", "Reviews code")

	bus := eventbus.New()
```
