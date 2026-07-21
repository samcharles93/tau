---
description: Source module internal/store/sqlite_store_test.go (453 lines).
resource: internal/store/sqlite_store_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: sqlite_store_test.go
type: Module
---

# Module sqlite_store_test.go

**Path**: `internal/store/sqlite_store_test.go`  
**Lines**: 453

## Snippet Preview

```
package store_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLiteStore_SaveAndLoad(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	state := chatSessionFixture("test-session", "claude-sonnet", "anthropic")
	state.Messages = []chat.ChatMessage{
		{Role: chat.ChatRoleSystem, Content: "You are helpful."},
		{Role: chat.ChatRoleUser, Content: "Hello"},
		{Role: chat.ChatRoleAssistant, Content: "Hi there!", ReasoningContent: "User said hello, I should greet them."},
	}
	state.LastUsage = chat.ChatUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
```
