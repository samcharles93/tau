---
description: Source module internal/agent/prompt.go (306 lines).
resource: internal/agent/prompt.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: prompt.go
type: Module
---

# Module prompt.go

**Path**: `internal/agent/prompt.go`  
**Lines**: 306

## Snippet Preview

```
package agent

import (
	_ "embed"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"text/template"
	"time"

	agentspec "github.com/samcharles93/tau/internal/agent/spec"
	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/skills"
)

//go:embed templates/agent.md.tpl
var agentPromptTpl string

// PromptConfig holds all inputs for building the system prompt.
type PromptConfig struct {
	// Tools are registered capability metadata exposed to the LLM. Tool
	// descriptions do not outrank the prompt's behavioral instructions.
	Tools []tools.Schema

	// Skills are the active skill catalog discovered at session start. Full
```
