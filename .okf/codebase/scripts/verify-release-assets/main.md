---
description: Source module scripts/verify-release-assets/main.go (123 lines).
resource: scripts/verify-release-assets/main.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: main.go
type: Module
---

# Module main.go

**Path**: `scripts/verify-release-assets/main.go`  
**Lines**: 123

## Snippet Preview

```
// Command verify-release-assets checks a goreleaser dist/ directory against
// internal/updater.SupportedTargets(), the single source of truth for the
// release platform matrix. It fails if any supported target's archive is
// missing from dist/, or if dist/checksums.txt has no entry for it.
//
// Intended to run in CI right after a goreleaser dry-run/build step and
// before the actual publish step, so a misconfigured goreleaser (a target
// dropped from builds.goos/goarch, a broken archive name template, etc.)
// fails CI instead of silently shipping an incomplete release.
//
// Usage:
//
//	go run ./scripts/verify-release-assets --dist dist --tag v1.2.3
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/samcharles93/tau/internal/updater"
)

func main() {
```
