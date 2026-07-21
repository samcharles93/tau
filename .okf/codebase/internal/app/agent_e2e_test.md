---
description: Source module internal/app/agent_e2e_test.go (291 lines).
resource: internal/app/agent_e2e_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: agent_e2e_test.go
type: Module
---

# Module agent_e2e_test.go

**Path**: `internal/app/agent_e2e_test.go`  
**Lines**: 291

## Snippet Preview

```
//go:build e2e

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/store"
)

var (
	tauBinary   string
	fakeBaseURL string
)

// TestMain builds tau once, starts a fake AI provider, and runs e2e tests.
func TestMain(m *testing.M) {
	// Build tau binary.
```
