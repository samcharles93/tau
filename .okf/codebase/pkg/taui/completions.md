---
description: Source module pkg/taui/completions.go (621 lines).
resource: pkg/taui/completions.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: completions.go
type: Module
---

# Module completions.go

**Path**: `pkg/taui/completions.go`  
**Lines**: 621

## Snippet Preview

```
package taui

import (
	"strings"
	"sync"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// ── Data model ───────────────────────────────────────────────────────────────

type Match struct {
	Word        string
	Display     string
	Description string
}

type MatchGroup struct {
	Title           string
	Matches         []Match
	NoTrailingSpace bool
}

type CompletionSet struct {
	Groups       []MatchGroup
	ReplaceStart int
	ReplaceEnd   int
}

type CompletionContext struct {
```
