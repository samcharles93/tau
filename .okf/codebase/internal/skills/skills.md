---
description: Source module internal/skills/skills.go (614 lines).
resource: internal/skills/skills.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: skills.go
type: Module
---

# Module skills.go

**Path**: `internal/skills/skills.go`  
**Lines**: 614

## Snippet Preview

```
package skills

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	tauconfig "github.com/samcharles93/tau/internal/config"
	"gopkg.in/yaml.v3"
)

const (
	SkillFileName          = "SKILL.md"
	SkillFileNameLower     = "skill.md"
	MaxNameLength          = 64
	MaxDescriptionLength   = 1024
	MaxCompatibilityLength = 500
	MaxInstructionsLength  = 10000
	maxDiscoveryDepth      = 6

	userInteropPriority    = 10
	userLegacyPriority     = 20
	userNativePriority     = 30
	projectInteropPriority = 110
```
