---
description: Source module internal/skills/tracker.go (75 lines).
resource: internal/skills/tracker.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: tracker.go
type: Module
---

# Module tracker.go

**Path**: `internal/skills/tracker.go`  
**Lines**: 75

## Snippet Preview

```
package skills

import (
	"strings"
	"sync"
)

// Tracker remembers which skills have been activated in the current session.
type Tracker struct {
	mu        sync.RWMutex
	activated map[string]struct{}
	order     []string
}

func NewTracker() *Tracker {
	return &Tracker{activated: make(map[string]struct{})}
}

func (t *Tracker) Activate(skill *Skill) {
	if t == nil || skill == nil {
		return
	}

	name := strings.TrimSpace(skill.Name)
	if name == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()
```
