---
description: Source module internal/registry/registry.go (157 lines).
resource: internal/registry/registry.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: registry.go
type: Module
---

# Module registry.go

**Path**: `internal/registry/registry.go`  
**Lines**: 157

## Snippet Preview

```
// Package registry discovers and publishes the set of available commands
// (built-in, custom, skill-based, and extension) so that consumers such as
// the TUI can render completions without hard-coding command lists.
//
// Extension commands from plugins arrive on the event bus separately via
// [chat.ExtensionCommandsChangedEvent] and are not duplicated here.
package registry

import (
	"slices"
	"sync"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/skills"
)

// Command is a self-describing command registered in the registry.
type Command struct {
	Name        string // unique key, e.g. "model", "session:list"
	Label       string // display label, e.g. "/model"
	Description string // one-line help text
	AcceptsArgs bool   // true if the command takes arguments
}

// Registry collects commands from all sources and publishes the merged
// set on the event bus so that consumers can stay up to date.
type Registry struct {
	mu        sync.RWMutex
	commands  map[string]Command
```
