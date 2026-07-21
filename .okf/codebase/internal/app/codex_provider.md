---
description: Source module internal/app/codex_provider.go (365 lines).
resource: internal/app/codex_provider.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: codex_provider.go
type: Module
---

# Module codex_provider.go

**Path**: `internal/app/codex_provider.go`  
**Lines**: 365

## Snippet Preview

```
package app

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	aisdkchat "github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/runtime"
)

type codexClass struct{}

func (codexClass) Name() string { return "openai-codex" }

func (codexClass) Supports(cap runtime.Capability) bool {
	return cap == runtime.CapabilityChat
}

func (codexClass) New(ctx context.Context, cfg runtime.ProviderConfig, model runtime.ModelInfo) (runtime.ProviderSet, error) {
	auth, err := runtime.ResolveAPIKey(cfg)
	if err != nil {
```
