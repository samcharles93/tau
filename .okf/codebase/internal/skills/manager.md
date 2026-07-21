---
description: Source module internal/skills/manager.go (96 lines).
resource: internal/skills/manager.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: manager.go
type: Module
---

# Module manager.go

**Path**: `internal/skills/manager.go`  
**Lines**: 96

## Snippet Preview

```
package skills

import (
	"errors"
	"sync"

	"github.com/samcharles93/tau/internal/eventbus"
)

// Event represents a refreshed skill snapshot.
type Event struct {
	AllSkills    []*Skill
	ActiveSkills []*Skill
	Diagnostics  []Diagnostic
}

// DiscoveryConfig controls a refresh pass.
type DiscoveryConfig struct {
	WorkingDir     string
	ExtraPaths     []string
	DisabledSkills []string
}

// Manager owns a workspace-scoped skill catalog and publishes refresh events
// through the event bus. Callers subscribe to [Event] on the bus directly.
type Manager struct {
	mu           sync.RWMutex
	allSkills    []*Skill
	activeSkills []*Skill
	diagnostics  []Diagnostic
```
