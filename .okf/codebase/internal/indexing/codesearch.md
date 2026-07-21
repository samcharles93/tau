---
description: Source module internal/indexing/codesearch.go (512 lines).
resource: internal/indexing/codesearch.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: codesearch.go
type: Module
---

# Module codesearch.go

**Path**: `internal/indexing/codesearch.go`  
**Lines**: 512

## Snippet Preview

```
// Package indexing provides language-neutral workspace text indexing.
package indexing

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"regexp/syntax"
	"slices"
	"strings"
	"time"

	"github.com/google/codesearch/index"
	"github.com/google/uuid"
	"github.com/samcharles93/tau/internal/config"

	_ "modernc.org/sqlite"
)

const (
	// MaxCandidateFiles bounds helper output and grep argv construction.
	MaxCandidateFiles    = 2000
```
