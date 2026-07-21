---
description: Source module internal/agent/tools/child_prompt.go (78 lines).
resource: internal/agent/tools/child_prompt.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: child_prompt.go
type: Module
---

# Module child_prompt.go

**Path**: `internal/agent/tools/child_prompt.go`  
**Lines**: 78

## Snippet Preview

```
package tools

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
	"time"

	"github.com/samcharles93/tau/internal/agent/spec"
)

// childPromptData mirrors internal/agent.promptData's field names exactly
// (a subset - WorkspaceTree/Tools/ContextFiles/Guidelines/SkillsIndex/
// AgentBody are root-system-prompt composition slots, not used by a plain
// spec body), so the same {{.WorkingDir}}-style template vars in spec
// bodies render identically whether the spec is the root's own identity or
// a spawned child's. Duplicated here rather than imported: internal/agent
// imports internal/agent/tools (for the tool registry), so tools importing
// internal/agent back would be a cycle.
type childPromptData struct {
	WorkingDir string
	Platform   string
	Shell      string
	Date       string
	IsGitRepo  bool
	ModelName  string
```
