---
description: Source module internal/agent/tools/stderr_redact_test.go (171 lines).
resource: internal/agent/tools/stderr_redact_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: stderr_redact_test.go
type: Module
---

# Module stderr_redact_test.go

**Path**: `internal/agent/tools/stderr_redact_test.go`  
**Lines**: 171

## Snippet Preview

```
package tools

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// --- G16: stderr redaction, rate limiting, capture cap ---

func TestRedactSecrets(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "api key env pattern",
			in:   "TAU_API_KEY=sk-abc123testtesttesttest",
			want: "TAU_API_KEY=[REDACTED]",
		},
		{
			name: "bearer token, spec's exact example",
			in:   "Authorization: Bearer tok_abc123",
			want: "Authorization: Bearer [REDACTED]",
		},
		{
			name: "bare sk- key",
```
