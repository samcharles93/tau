---
description: Source module internal/chat/types.go (1487 lines).
resource: internal/chat/types.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: types.go
type: Module
---

# Module types.go

**Path**: `internal/chat/types.go`  
**Lines**: 1487

## Snippet Preview

```
package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/samcharles93/tau/internal/config"
)

const (
	defaultChatSystemPrompt = "You are a helpful assistant."
	defaultChatMaxTokens    = 0
	defaultChatTemperature  = 0.7
)

func DefaultParameters() ChatParameters {
	return defaultChatParameters()
}

// ClampMaxTokensForModel caps a requested output-token budget to the selected
// model's configured output ceiling. A zero requested value means provider
// default and stays zero.
func ClampMaxTokensForModel(maxTokens int, model ChatModelRef) int {
	if maxTokens <= 0 {
		return maxTokens
	}
```
