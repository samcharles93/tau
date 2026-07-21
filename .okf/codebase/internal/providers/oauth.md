---
description: Source module internal/providers/oauth.go (730 lines).
resource: internal/providers/oauth.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: oauth.go
type: Module
---

# Module oauth.go

**Path**: `internal/providers/oauth.go`  
**Lines**: 730

## Snippet Preview

```
package providers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	githubCopilotID = "github-copilot"
	openAICodexID   = "openai-codex"

	githubCopilotClientID = "Iv1.b507a08c87ecfe98"

	// OpenAI Codex does not implement RFC 8628 device authorization (there is
	// no /oauth/device/code grant for this client). ChatGPT sign-in uses a
	// proprietary three-step flow instead: request a user code, poll for an
	// authorization code, then exchange that code via a normal PKCE
	// authorization_code grant. client_id is the Codex CLI's own public
```
