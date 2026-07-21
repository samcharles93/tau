---
description: Source module internal/cli/commands_test.go (78 lines).
resource: internal/cli/commands_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: commands_test.go
type: Module
---

# Module commands_test.go

**Path**: `internal/cli/commands_test.go`  
**Lines**: 78

## Snippet Preview

```
package cli

import (
	"testing"

	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/providers"
)

func TestUnavailableProviderErrorExplainsMissingEnvVar(t *testing.T) {
	resolved := []providers.ResolvedProvider{
		{
			Config:    config.ProviderConfig{Name: "anthropic"},
			Source:    providers.SourceEnv,
			Available: false,
			Message:   "ANTHROPIC_API_KEY is not set",
		},
	}

	got := unavailableProviderError(resolved, "anthropic", "")
	want := `provider "anthropic" is configured but not currently usable: ANTHROPIC_API_KEY is not set`
	if got != want {
		t.Errorf("unavailableProviderError() = %q, want %q", got, want)
	}
}

func TestUnavailableProviderErrorFallsBackToDefaultProvider(t *testing.T) {
	resolved := []providers.ResolvedProvider{
		{Config: config.ProviderConfig{Name: "anthropic"}, Available: false, Message: "ANTHROPIC_API_KEY is not set"},
	}
```
