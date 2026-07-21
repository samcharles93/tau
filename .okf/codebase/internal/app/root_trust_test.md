---
description: Source module internal/app/root_trust_test.go (254 lines).
resource: internal/app/root_trust_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: root_trust_test.go
type: Module
---

# Module root_trust_test.go

**Path**: `internal/app/root_trust_test.go`  
**Lines**: 254

## Snippet Preview

```
package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/samcharles93/tau/internal/agent/spec"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/skills"
	"github.com/samcharles93/tau/internal/trust"
)

// withIsolatedConfigDir points HOME/XDG_CONFIG_HOME at a temp directory for
// the duration of the test, so trust.yaml reads/writes - and UserSources()
// discovery - never touch the real user's machine. Returns the resolved
// tau config dir.
func withIsolatedConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	return config.Dir()
}

// writeProjectRootSpec writes a project-level tau.agent.md override at the
// path spec.ProjectSources(cwd) actually discovers
// (<cwd>/.agents/agents/tau.agent.md), so resolveRootSpecWithTrust's real
// call to agent.ResolveRootSpec(cwd) picks it up exactly as production
// startup would.
```
