---
description: Source module internal/app/streamer.go (272 lines).
resource: internal/app/streamer.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: streamer.go
type: Module
---

# Module streamer.go

**Path**: `internal/app/streamer.go`  
**Lines**: 272

## Snippet Preview

```
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	aisdkchat "github.com/samcharles93/ai-sdk/chat"
	tauchat "github.com/samcharles93/tau/internal/chat"
)

// providerResolver resolves the ai-sdk provider and model ID to use for a given
// session. A static resolver (NewStreamer) always returns the same provider; a
// dynamic resolver (NewDynamicStreamer) picks the provider per call from the
// session's selected provider/model, which is what enables live cross-provider
// switching during a chat.
type providerResolver func(ctx context.Context, session tauchat.ChatSessionState) (aisdkchat.Provider, string, error)

// Streamer adapts an ai-sdk chat.Provider into tau's agent.Streamer
// interface (StreamChatCompletionFull). It preserves tau's existing
// coordinator turn loop, event bus, and tool execution while delegating the
// raw provider protocol to ai-sdk.
type Streamer struct {
	resolve providerResolver
}

```
