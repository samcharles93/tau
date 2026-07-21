---
description: Source module internal/trust/trust.go (141 lines).
resource: internal/trust/trust.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: trust.go
type: Module
---

# Module trust.go

**Path**: `internal/trust/trust.go`  
**Lines**: 141

## Snippet Preview

```
// Package trust implements the trust-on-first-use store for project-level
// root-spec overrides, per docs/specs/agents/01-agent-spec-format.md
// (Root-spec override trust). A project directory can ship a
// tau.agent.md that replaces the root agent's identity while it retains
// the full tool registry - a privilege-escalation vector when the project
// is untrusted (e.g. a freshly cloned repository). The store records which
// project + spec-hash combinations the user has explicitly approved so the
// approval doesn't need to be repeated every run, while any change to the
// spec file (a different hash) is treated as untrusted again.
package trust

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// ModeHash trusts only the exact spec_hash recorded - any edit to the spec
// file requires re-approval.
const ModeHash = "hash"

// ModePath trusts any hash for the given project_path - future edits to
// the spec file are trusted without re-prompting.
const ModePath = "path"

```
