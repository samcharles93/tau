---
description: Source module internal/updater/updater.go (318 lines).
resource: internal/updater/updater.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: updater.go
type: Module
---

# Module updater.go

**Path**: `internal/updater/updater.go`  
**Lines**: 318

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
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/minio/selfupdate"
)

const (
	DefaultRepo       = "samcharles93/tau"
	defaultAPIBaseURL = "https://api.github.com"
	userAgent         = "tau"
)

var ErrNoUpdate = errors.New("tau is already up to date")
```
