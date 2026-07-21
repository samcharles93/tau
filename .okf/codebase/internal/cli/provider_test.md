---
description: Source module internal/cli/provider_test.go (87 lines).
resource: internal/cli/provider_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: provider_test.go
type: Module
---

# Module provider_test.go

**Path**: `internal/cli/provider_test.go`  
**Lines**: 87

## Snippet Preview

```
package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samcharles93/tau/internal/app"
)

func TestRenderProviderListShowsStatusSourceAndAuth(t *testing.T) {
	var output bytes.Buffer
	renderProviderList(&output, []app.ProviderStatus{
		{ID: "openai", Status: "enabled", Source: "managed", Auth: "api_key", Details: "stored key"},
		{ID: "github-copilot", Status: "disabled", Source: "oauth", Auth: "oauth"},
	})

	text := output.String()
	for _, want := range []string{"PROVIDER", "STATUS", "SOURCE", "AUTH", "openai", "enabled", "managed", "api_key", "stored key", "github-copilot", "disabled", "oauth"} {
		assert.Contains(t, text, want)
	}
}

func TestProviderLoginNameRequiresNameOffTTY(t *testing.T) {
```
