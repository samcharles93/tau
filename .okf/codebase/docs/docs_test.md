---
description: Source module docs/docs_test.go (113 lines).
resource: docs/docs_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: docs_test.go
type: Module
---

# Module docs_test.go

**Path**: `docs/docs_test.go`  
**Lines**: 113

## Snippet Preview

```
package docs

import (
	"io/fs"
	"os"
	"strings"
	"testing"
)

func TestEmbeddedFS(t *testing.T) {
	var foundConfigExample bool
	var foundReadme bool
	var foundPlugins bool
	var foundMCPExample bool
	var foundWebUISpec bool

	err := fs.WalkDir(FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if path == "." {
			return nil
		}

		// Validate: No .go files are embedded in FS
		if strings.HasSuffix(path, ".go") {
			t.Errorf("found embedded .go file: %s", path)
		}

```
