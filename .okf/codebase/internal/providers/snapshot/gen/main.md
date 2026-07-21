---
description: Source module internal/providers/snapshot/gen/main.go (205 lines).
resource: internal/providers/snapshot/gen/main.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: main.go
type: Module
---

# Module main.go

**Path**: `internal/providers/snapshot/gen/main.go`  
**Lines**: 205

## Snippet Preview

```
// Command gen builds tau's embedded provider+model snapshot from models.dev.
//
// It fetches the models.dev catalogue (or reads a local copy with -input),
// keeps only the providers in tau's built-in catalog (remapping ids where
// models.dev disagrees, e.g. gemini←google), and within each keeps only the
// models that advertise tool calling. The result is written in the models.dev
// JSON shape so the ai-sdk runtime can load it directly, and embedded into the
// binary so tau never depends on models.dev being reachable at runtime.
//
// Usage:
//
//	go run ./internal/providers/snapshot/gen            # fetch live
//	go run ./internal/providers/snapshot/gen -input f   # from a local file
//	go run ./internal/providers/snapshot/gen -o path    # custom output
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/samcharles93/tau/internal/providers"
)

```
