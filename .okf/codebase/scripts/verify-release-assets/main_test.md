---
description: Source module scripts/verify-release-assets/main_test.go (144 lines).
resource: scripts/verify-release-assets/main_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: main_test.go
type: Module
---

# Module main_test.go

**Path**: `scripts/verify-release-assets/main_test.go`  
**Lines**: 144

## Snippet Preview

```
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samcharles93/tau/internal/updater"
)

// writeCompleteMatrix populates dir with an archive for every supported
// target plus a checksums.txt entry recording that archive's real SHA-256,
// all at the given tag. Real hashes (not placeholders) so tests exercise
// run()'s actual hash-comparison path, not just presence checks.
func writeCompleteMatrix(t *testing.T, dir, tag string) {
	t.Helper()

	var checksums strings.Builder
	for _, target := range updater.SupportedTargets() {
		name, err := updater.ArchiveName(tag, target.OS, target.Arch)
		if err != nil {
			t.Fatalf("ArchiveName(%s, %s, %s): %v", tag, target.OS, target.Arch, err)
		}
		content := []byte("fake archive for " + name)
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatalf("write archive %s: %v", name, err)
```
