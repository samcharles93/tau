---
description: Source module internal/cli/prompt.go (162 lines).
resource: internal/cli/prompt.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: prompt.go
type: Module
---

# Module prompt.go

**Path**: `internal/cli/prompt.go`  
**Lines**: 162

## Snippet Preview

```
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// ErrPromptCanceled is returned by the prompt helpers in this file when ctx
// is canceled (e.g. Ctrl+C, wired to signal cancellation upstream) while
// waiting for input.
var ErrPromptCanceled = errors.New("prompt canceled")

// SelectOption is one entry in a numbered selection list presented by
// Select.
type SelectOption struct {
	Label string // shown to the user
	Value string // returned when this option is chosen
}

// Select renders options as a numbered list on w, prompts on w, and reads a
// numeric choice from r, re-prompting on non-numeric or out-of-range input.
// It returns ErrPromptCanceled if ctx is canceled before a valid choice is
// read. There is no portable way to interrupt a blocking read on r once
```
