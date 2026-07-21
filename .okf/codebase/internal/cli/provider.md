---
description: Source module internal/cli/provider.go (158 lines).
resource: internal/cli/provider.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: provider.go
type: Module
---

# Module provider.go

**Path**: `internal/cli/provider.go`  
**Lines**: 158

## Snippet Preview

```
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/samcharles93/tau/internal/app"
	urfavecli "github.com/urfave/cli/v3"
	"golang.org/x/term"
)

var errProviderUsage = errors.New("provider command usage error")

type providerUsageError struct {
	message string
}

func (e providerUsageError) Error() string { return e.message }
func (e providerUsageError) Unwrap() error { return errProviderUsage }

func providerUsage(message string) error {
	return providerUsageError{message: message}
}

func providerCmd() *urfavecli.Command {
	return &urfavecli.Command{
```
