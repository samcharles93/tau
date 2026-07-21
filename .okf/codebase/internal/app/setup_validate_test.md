---
description: Source module internal/app/setup_validate_test.go (168 lines).
resource: internal/app/setup_validate_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: setup_validate_test.go
type: Module
---

# Module setup_validate_test.go

**Path**: `internal/app/setup_validate_test.go`  
**Lines**: 168

## Snippet Preview

```
package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samcharles93/tau/internal/providers"
)

func TestLiveValidateAPIKeyOpenAICompatibleValidKey(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	entry := providers.CatalogEntry{ID: "openai", DisplayName: "OpenAI", BaseURL: srv.URL + "/v1", Auth: providers.AuthAPIKey}
	result := liveValidateAPIKey(context.Background(), entry, "sk-test-key", false)

	require.Equal(t, apiKeyValid, result.outcome)
	assert.Equal(t, "/v1/models", gotPath)
	assert.Equal(t, "Bearer sk-test-key", gotAuth)
```
