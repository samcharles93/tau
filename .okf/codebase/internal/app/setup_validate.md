---
description: Source module internal/app/setup_validate.go (129 lines).
resource: internal/app/setup_validate.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: setup_validate.go
type: Module
---

# Module setup_validate.go

**Path**: `internal/app/setup_validate.go`  
**Lines**: 129

## Snippet Preview

```
package app

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/samcharles93/tau/internal/providers"
)

// apiKeyValidationTimeout bounds the live credential probe RunSetup performs
// before persisting an API key, so an unreachable or slow provider fails fast
// instead of stalling the setup flow. A var, not a const, so tests can shrink
// it to keep a deliberately-slow-server test fast.
var apiKeyValidationTimeout = 5 * time.Second

// anthropicAPIVersion mirrors the version ai-sdk's native Anthropic client
// sends (provider/anthropic/anthropic.go) so the validation probe is
// accepted by the same API surface the runtime actually uses.
const anthropicAPIVersion = "2023-06-01"

// apiKeyValidationOutcome classifies the result of a live API-key probe.
type apiKeyValidationOutcome int

const (
	// apiKeyValid means the provider accepted the credential.
	apiKeyValid apiKeyValidationOutcome = iota
```
