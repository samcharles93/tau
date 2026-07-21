---
description: Source module internal/registry/registry_test.go (174 lines).
resource: internal/registry/registry_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: registry_test.go
type: Module
---

# Module registry_test.go

**Path**: `internal/registry/registry_test.go`  
**Lines**: 174

## Snippet Preview

```
package registry

import (
	"testing"

	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/skills"
)

func TestDiscover(t *testing.T) {
	bus := eventbus.New()
	defer bus.Close()

	client := bus.Client("test-registry")
	defer client.Close()

	reg := New("/nonexistent", client)
	reg.Discover()

	// Built-in commands are always present regardless of custom command
	// directories. Verify the built-in set is non-empty.
	cmds := reg.All()
	if len(cmds) == 0 {
		t.Fatal("expected built-in commands after Discover, got 0")
	}
	// Spot-check a well-known built-in.
	if cmd, ok := reg.Lookup("model"); !ok {
		t.Error("expected built-in 'model' command")
	} else if cmd.AcceptsArgs != true {
		t.Errorf("model AcceptsArgs = %v, want true", cmd.AcceptsArgs)
```
