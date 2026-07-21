---
description: Source module internal/updater/updater_test.go (215 lines).
resource: internal/updater/updater_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: updater_test.go
type: Module
---

# Module updater_test.go

**Path**: `internal/updater/updater_test.go`  
**Lines**: 215

## Snippet Preview

```
package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSupportedTargets(t *testing.T) {
	t.Parallel()

	targets := SupportedTargets()
	require.ElementsMatch(t, []Target{
		{OS: "darwin", Arch: "amd64"},
		{OS: "darwin", Arch: "arm64"},
		{OS: "linux", Arch: "amd64"},
		{OS: "linux", Arch: "arm64"},
		{OS: "windows", Arch: "amd64"},
```
