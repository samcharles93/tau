---
description: Source module internal/skills/manager_test.go (60 lines).
resource: internal/skills/manager_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: manager_test.go
type: Module
---

# Module manager_test.go

**Path**: `internal/skills/manager_test.go`  
**Lines**: 60

## Snippet Preview

```
package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/stretchr/testify/require"
)

func writeSkillFile(t *testing.T, baseDir string, name string, content string) {
	skillDir := filepath.Join(baseDir, name)
	err := os.MkdirAll(filepath.Join(skillDir, "scripts"), 0o755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644)
	require.NoError(t, err)
}

func TestManagerRefreshPublishesSnapshot(t *testing.T) {
	configDir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("TAU_CONFIG_DIR", configDir)
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	workingDir := t.TempDir()

	writeSkillFile(t, filepath.Join(configDir, "skills"), "pdf-processing", `---
name: pdf-processing
description: Extract PDF text. Use when the user mentions PDFs.
```
