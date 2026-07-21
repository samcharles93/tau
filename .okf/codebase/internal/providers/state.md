---
description: Source module internal/providers/state.go (276 lines).
resource: internal/providers/state.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: state.go
type: Module
---

# Module state.go

**Path**: `internal/providers/state.go`  
**Lines**: 276

## Snippet Preview

```
package providers

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/samcharles93/tau/internal/config"
)

// stateFileName is the tau-owned managed file holding enabled providers and
// OAuth credentials. It lives alongside config.yaml but is written exclusively
// by tau, so the user's hand-edited config is never disturbed.
const stateFileName = "auth.yaml"

const stateVersion = 1

// OAuthCredentials holds persisted credentials for an OAuth provider. Expires is
// a Unix timestamp in seconds; zero means "unknown / never expires". Extra
// carries provider-specific fields (e.g. a derived API base URL) without
// bloating the core schema.
type OAuthCredentials struct {
```
