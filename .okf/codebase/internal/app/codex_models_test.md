---
description: Source module internal/app/codex_models_test.go (66 lines).
resource: internal/app/codex_models_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: codex_models_test.go
type: Module
---

# Module codex_models_test.go

**Path**: `internal/app/codex_models_test.go`  
**Lines**: 66

## Snippet Preview

```
package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	tauconfig "github.com/samcharles93/tau/internal/config"
)

func TestCodexModelsRequestsCodexModelsPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(codexModelsResponse{Models: []codexModelInfo{{Slug: "gpt-5.5"}}})
	}))
	defer srv.Close()

	provider := tauconfig.ProviderConfig{Name: "openai-codex", BaseURL: srv.URL + "/backend-api"}
	models, err := codexModels(context.Background(), provider, false)
	if err != nil {
		t.Fatalf("codexModels() error = %v", err)
	}
	if gotPath != "/backend-api/codex/models" {
		t.Fatalf("requested path = %q, want /backend-api/codex/models", gotPath)
	}
	if len(models) != 1 || models[0].Slug != "gpt-5.5" {
		t.Fatalf("models = %#v, want one gpt-5.5 entry", models)
```
