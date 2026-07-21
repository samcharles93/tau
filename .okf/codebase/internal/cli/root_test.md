---
description: Source module internal/cli/root_test.go (148 lines).
resource: internal/cli/root_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: root_test.go
type: Module
---

# Module root_test.go

**Path**: `internal/cli/root_test.go`  
**Lines**: 148

## Snippet Preview

```
package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	urfavecli "github.com/urfave/cli/v3"
)

func TestSplitProviderModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		raw      string
		provider string
		model    string
		ok       bool
	}{
		{
			name:     "preferred colon syntax with nested model path",
			raw:      "openrouter:nvidia/nemotron-3-ultra",
			provider: "openrouter",
			model:    "nvidia/nemotron-3-ultra",
			ok:       true,
		},
```
