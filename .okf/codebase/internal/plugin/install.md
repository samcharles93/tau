---
description: Source module internal/plugin/install.go (469 lines).
resource: internal/plugin/install.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: install.go
type: Module
---

# Module install.go

**Path**: `internal/plugin/install.go`  
**Lines**: 469

## Snippet Preview

```
package plugin

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/plugin/registry"
)

// InstalledPlugin represents a plugin binary found on disk.
type InstalledPlugin struct {
	Name string
	Size int64
}

// Install downloads a plugin binary from the registry and places it in the
// plugins directory. If version is empty, the latest version is resolved.
```
