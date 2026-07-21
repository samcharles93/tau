---
description: Source module internal/cli/prompt_test.go (149 lines).
resource: internal/cli/prompt_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: prompt_test.go
type: Module
---

# Module prompt_test.go

**Path**: `internal/cli/prompt_test.go`  
**Lines**: 149

## Snippet Preview

```
package cli

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func providerOptions() []SelectOption {
	return []SelectOption{
		{Label: "Anthropic (Claude)", Value: "anthropic"},
		{Label: "OpenAI", Value: "openai"},
		{Label: "Ollama (local)", Value: "ollama"},
	}
}

func TestSelectRendersNumberedListAndPicksValidChoice(t *testing.T) {
	var out strings.Builder
	got, err := Select(context.Background(), &out, strings.NewReader("2\n"), "Providers", providerOptions())
	require.NoError(t, err)
	assert.Equal(t, "openai", got.Value)

	rendered := out.String()
	assert.Contains(t, rendered, "1) Anthropic (Claude)")
```
