---
description: Source module internal/agent/spec/resolve_test.go (87 lines).
resource: internal/agent/spec/resolve_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: resolve_test.go
type: Module
---

# Module resolve_test.go

**Path**: `internal/agent/spec/resolve_test.go`  
**Lines**: 87

## Snippet Preview

```
package spec

import (
	"path/filepath"
	"testing"

	"github.com/samcharles93/tau/internal/skills"
)

func TestResolveBuiltinByBareName(t *testing.T) {
	def, ok := Resolve("plan", "")
	if !ok {
		t.Fatal("expected built-in 'plan' to resolve")
	}
	if def.SourcePath != "" {
		t.Errorf("SourcePath = %q, want empty for a built-in", def.SourcePath)
	}
}

func TestResolveDiscoveredProjectPrefixed(t *testing.T) {
	projectDir := t.TempDir()
	writeDef(t, filepath.Join(projectDir, ".agents", "agents"), "reviewer", "Reviews code")

	def, ok := Resolve("project:reviewer", projectDir)
	if !ok {
		t.Fatal("expected project:reviewer to resolve")
	}
	if def.Name != "reviewer" {
		t.Errorf("Name = %q, want reviewer", def.Name)
	}
```
