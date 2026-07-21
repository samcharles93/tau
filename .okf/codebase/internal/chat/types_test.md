---
description: Source module internal/chat/types_test.go (387 lines).
resource: internal/chat/types_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: types_test.go
type: Module
---

# Module types_test.go

**Path**: `internal/chat/types_test.go`  
**Lines**: 387

## Snippet Preview

```
package chat

import (
	"errors"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/config"
)

func testProvider() config.ProviderConfig {
	return config.ProviderConfig{
		Name:    "test",
		BaseURL: "https://provider.example",
		Auth:    config.AuthConfig{Type: config.AuthTypeNone},
	}
}

// TestChatSessionConfigValidateAllowsUnconfiguredProvider guards a real
// startup path: `tau --skip-setup` on a machine with no providers must be
// able to launch the TUI showing "use /provider" guidance rather than
// hard-failing before the app ever appears, the same way an empty model
// already launches unselected with a "use /model" hint.
func TestChatSessionConfigValidateAllowsUnconfiguredProvider(t *testing.T) {
	cfg := ChatSessionConfig{}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil for a fully empty (unconfigured) provider+model", err)
	}
}

```
