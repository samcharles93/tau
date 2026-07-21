---
description: Source module internal/skills/skills_test.go (49 lines).
resource: internal/skills/skills_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: skills_test.go
type: Module
---

# Module skills_test.go

**Path**: `internal/skills/skills_test.go`  
**Lines**: 49

## Snippet Preview

```
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateSkill_LongInstructions(t *testing.T) {
	tempDir := t.TempDir()
	skillDir := filepath.Join(tempDir, "test-skill")
	err := os.MkdirAll(filepath.Join(skillDir, "scripts"), 0o755)
	require.NoError(t, err)

	// Create a SKILL.md with instructions exceeding MaxInstructionsLength
	longInstructions := strings.Repeat("a", MaxInstructionsLength+1)
	skillContent := fmt.Sprintf(`---
name: test-long-instructions
description: Test skill with overly long instructions
compatibility: tau
---

# Test Long Instructions

%s`, longInstructions)

```
