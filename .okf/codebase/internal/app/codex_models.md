---
description: Source module internal/app/codex_models.go (149 lines).
resource: internal/app/codex_models.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: codex_models.go
type: Module
---

# Module codex_models.go

**Path**: `internal/app/codex_models.go`  
**Lines**: 149

## Snippet Preview

```
package app

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
)

const codexModelsTimeout = 8 * time.Second

func codexModelRefs(ctx context.Context, provider tauconfig.ProviderConfig, insecure bool) ([]tauchat.ChatModelRef, error) {
	models, err := codexModels(ctx, provider, insecure)
	if err != nil {
		return nil, err
	}
	return codexModelRefsFromInfos(provider, models), nil
}

func codexModelRefsFromInfos(provider tauconfig.ProviderConfig, models []codexModelInfo) []tauchat.ChatModelRef {
	refs := make([]tauchat.ChatModelRef, 0, len(models))
	for _, m := range models {
		id := strings.TrimSpace(firstNonEmpty(m.Slug, m.ID, m.Model))
		if id == "" {
```
