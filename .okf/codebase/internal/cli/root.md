---
description: Source module internal/cli/root.go (351 lines).
resource: internal/cli/root.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: root.go
type: Module
---

# Module root.go

**Path**: `internal/cli/root.go`  
**Lines**: 351

## Snippet Preview

```
package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/samcharles93/tau/internal/app"
	tauconfig "github.com/samcharles93/tau/internal/config"
	taulogger "github.com/samcharles93/tau/internal/logger"
	urfavecli "github.com/urfave/cli/v3"
	"golang.org/x/term"
)

func initLogging(debug bool, version string) {
	logPath := filepath.Join(tauconfig.Dir(), "tau.log")

	// Rotate the log if it has grown too large (10 MiB). Keep one old copy.
	const maxSize = 10 * 1024 * 1024
	if info, err := os.Stat(logPath); err == nil && info.Size() > maxSize {
		rotated := logPath + ".1"
		_ = os.Remove(rotated)
		_ = os.Rename(logPath, rotated)
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
```
