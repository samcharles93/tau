---
description: Source module internal/agent/spec/identity.go (166 lines).
resource: internal/agent/spec/identity.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: identity.go
type: Module
---

# Module identity.go

**Path**: `internal/agent/spec/identity.go`  
**Lines**: 166

## Snippet Preview

```
package spec

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/samcharles93/tau/internal/skills"
)

// HashSpecSnapshot returns the hex-encoded SHA-256 of a spec snapshot JSON string.
// The snapshot includes all frontmatter fields (tools, model, description), so
// a change to any of them produces a different hash - correctly detecting spec
// drift rather than silently colliding on identical bodies.
func HashSpecSnapshot(snapshotJSON string) string {
	h := sha256.Sum256([]byte(snapshotJSON))
	return fmt.Sprintf("%x", h[:])
}

// BuildSpecSnapshot serialises a resolved definition into a JSON snapshot
// suitable for storage in agent_instances.spec_snapshot.
func BuildSpecSnapshot(def *Definition, provider, model string, tools []string) string {
	snap := map[string]any{
		"name":        def.Name,
		"description": def.Description,
		"body":        def.Body,
		"resolved": map[string]any{
			"provider": provider,
```
