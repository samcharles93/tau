---
description: Source module internal/agent/prompt_test.go (229 lines).
resource: internal/agent/prompt_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: prompt_test.go
type: Module
---

# Module prompt_test.go

**Path**: `internal/agent/prompt_test.go`  
**Lines**: 229

## Snippet Preview

```
package agent

import (
	"html"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/skills"
)

func TestBuildSystemPromptMinimal(t *testing.T) {
	prompt := BuildSystemPrompt(PromptConfig{CWD: t.TempDir()})

	assertPromptContains(
		t, prompt,
		"<instruction_precedence>",
		"<core_rules>",
		"<communication>",
		"<confirmation_boundaries>",
		"<env purpose=\"runtime_metadata\" trust=\"data\">",
	)
	for _, section := range []string{
		"<tools ",
		"<guidelines ",
		"<project_context ",
```
