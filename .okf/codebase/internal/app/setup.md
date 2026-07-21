---
description: Source module internal/app/setup.go (303 lines).
resource: internal/app/setup.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: setup.go
type: Module
---

# Module setup.go

**Path**: `internal/app/setup.go`  
**Lines**: 303

## Snippet Preview

```
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/providers"
	"github.com/samcharles93/tau/internal/providerui"
)

// ErrSetupCanceled is returned by RunSetup when the interactive flow is
// canceled (e.g. Ctrl+C) at any step. Callers (e.g. the CLI) can match on
// this to distinguish a clean user cancellation from a real failure.
var ErrSetupCanceled = errors.New("setup canceled")

// setupMaxModelOptions caps the number of models offered in the default-model
// picker. Some catalog providers (e.g. OpenRouter) expose far more
// tool-capable models than fit in a usable numbered CLI list.
const setupMaxModelOptions = 20

// SetupOption is one entry a SetupPrompter presents for the user to choose
// from. It mirrors internal/cli's SelectOption without this package
// depending on internal/cli, which already depends on this package.
type SetupOption struct {
	Label string
```
