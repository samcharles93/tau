---
description: Source module internal/chat/commands/commands_test.go (101 lines).
resource: internal/chat/commands/commands_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: commands_test.go
type: Module
---

# Module commands_test.go

**Path**: `internal/chat/commands/commands_test.go`  
**Lines**: 101

## Snippet Preview

```
package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/samcharles93/tau/internal/skills"
	"github.com/stretchr/testify/require"
)

func TestLoadFromSource_NonExistentDir(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "does-not-exist")

	cmds, err := loadFromSource(commandSource{path: dir, prefix: userCommandPrefix})
	require.NoError(t, err)
	require.Empty(t, cmds)

	// directory must NOT have been created
	_, statErr := os.Stat(dir)
	require.True(t, os.IsNotExist(statErr))
}

func TestLoadFromSource_ExistingDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hello.md"), []byte("say hello"), 0o644))
```
