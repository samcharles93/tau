---
description: Source module internal/providers/oauth_test.go (436 lines).
resource: internal/providers/oauth_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: oauth_test.go
type: Module
---

# Module oauth_test.go

**Path**: `internal/providers/oauth_test.go`  
**Lines**: 436

## Snippet Preview

```
package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFriendlyOpenAIDeviceCodeErrorFor403(t *testing.T) {
	err := friendlyOpenAIDeviceCodeError(httpStatusError{StatusCode: http.StatusForbidden})
	if err == nil {
		t.Fatal("expected error")
	}
	got := err.Error()
	for _, want := range []string{
		"OpenAI rejected Tau's Codex device-code login start request",
		"Tau cannot complete this login flow right now",
		"No credentials were saved",
		"official Codex CLI login",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q to contain %q", got, want)
		}
```
