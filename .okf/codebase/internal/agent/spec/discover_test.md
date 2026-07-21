---
description: Source module internal/agent/spec/discover_test.go (96 lines).
resource: internal/agent/spec/discover_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: discover_test.go
type: Module
---

# Module discover_test.go

**Path**: `internal/agent/spec/discover_test.go`  
**Lines**: 96

## Snippet Preview

```
package spec

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/samcharles93/tau/internal/skills"
)

func writeDef(t *testing.T, dir, name, description string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, name+agentFileSuffix), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestDiscoverFromDiskParsesValidDefinition(t *testing.T) {
	dir := t.TempDir()
	writeDef(t, dir, "reviewer", "Reviews code")

	defs, diags := DiscoverFromDisk([]Source{{Root: dir, Scope: skills.ScopeProject, Priority: projectSourcePriority}})
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diags)
	}
	if len(defs) != 1 {
```
