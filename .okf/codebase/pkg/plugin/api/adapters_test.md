---
description: Source module pkg/plugin/api/adapters_test.go (828 lines).
resource: pkg/plugin/api/adapters_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: adapters_test.go
type: Module
---

# Module adapters_test.go

**Path**: `pkg/plugin/api/adapters_test.go`  
**Lines**: 828

## Snippet Preview

```
package api

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestProtoViewToChatNil(t *testing.T) {
	require.Nil(t, ProtoViewToChat(nil))
}

func TestProtoWidgetToChatNil(t *testing.T) {
	require.Equal(t, chat.Widget{}, ProtoWidgetToChat(nil))
}

func TestProtoWidgetToChatUnknownKindFallsBackToZeroValue(t *testing.T) {
	// A Widget with no oneof case set (e.g. a kind this host doesn't know
	// about yet) must render as nothing, not panic or default to some kind.
	got := ProtoWidgetToChat(&Widget{})
	require.Equal(t, chat.Widget{}, got)
}
```
