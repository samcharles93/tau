---
description: Source module internal/updater/targets.go (58 lines).
resource: internal/updater/targets.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: targets.go
type: Module
---

# Module targets.go

**Path**: `internal/updater/targets.go`  
**Lines**: 58

## Snippet Preview

```
package updater

import (
	"fmt"
	"strings"
)

// Target is a supported release build target: a GOOS/GOARCH pair that
// .goreleaser.yaml actually publishes an archive for.
//
// This is the single source of truth for the platform matrix. install.sh
// and install.ps1 can't import it directly (they run before Go is on the
// machine), so they mirror it inline - keep those in sync by hand whenever
// this list changes.
type Target struct {
	OS   string
	Arch string
}

// SupportedTargets returns every GOOS/GOARCH pair published in a release.
// Must match .goreleaser.yaml's builds.goos/goarch matrix exactly.
func SupportedTargets() []Target {
	return []Target{
		{OS: "darwin", Arch: "amd64"},
		{OS: "darwin", Arch: "arm64"},
		{OS: "linux", Arch: "amd64"},
		{OS: "linux", Arch: "arm64"},
		{OS: "windows", Arch: "amd64"},
		{OS: "windows", Arch: "arm64"},
	}
```
