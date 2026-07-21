---
description: Source module internal/tui2/completions_test.go (1213 lines).
resource: internal/tui2/completions_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: completions_test.go
type: Module
---

# Module completions_test.go

**Path**: `internal/tui2/completions_test.go`  
**Lines**: 1213

## Snippet Preview

```
package tui2

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
)

// modelsFor builds a test model with a fixed, deterministic set of available
// models - argument completion (/model <partial>) is used throughout these
// tests instead of command-name completion because the command table
// (slashTable) is real global state populated by commands.go's init(), so
// asserting exact fuzzy-match sets against it would be fragile.
func modelsFor(ids ...string) *model {
	m := newTestModel(&fakeRuntime{}, nil)
	refs := make([]tauchat.ChatModelRef, len(ids))
	for i, id := range ids {
		refs[i] = tauchat.ChatModelRef{ID: id}
	}
	m.availableModels = refs
	return m
}

```
