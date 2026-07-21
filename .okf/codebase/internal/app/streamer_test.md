---
description: Source module internal/app/streamer_test.go (287 lines).
resource: internal/app/streamer_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: streamer_test.go
type: Module
---

# Module streamer_test.go

**Path**: `internal/app/streamer_test.go`  
**Lines**: 287

## Snippet Preview

```
package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	aisdkchat "github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/runtime"

	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
)

type channelTestProvider struct {
	chunks []aisdkchat.Chunk
}

func (p *channelTestProvider) Name() string { return "channel-test" }

func (p *channelTestProvider) Chat(context.Context, aisdkchat.Request) (aisdkchat.Response, error) {
	return aisdkchat.Response{}, nil
}

func (p *channelTestProvider) ChatStream(context.Context, aisdkchat.Request) (aisdkchat.Stream, error) {
	return &channelTestStream{chunks: p.chunks}, nil
```
