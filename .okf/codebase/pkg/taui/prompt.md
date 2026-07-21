---
description: Source module pkg/taui/prompt.go (163 lines).
resource: pkg/taui/prompt.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: prompt.go
type: Module
---

# Module prompt.go

**Path**: `pkg/taui/prompt.go`  
**Lines**: 163

## Snippet Preview

```
package taui

import (
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// PromptKind distinguishes a yes/no confirmation from a free-text question.
type PromptKind int

const (
	PromptConfirm PromptKind = iota
	PromptQuestion
)

// Prompt is a small modal-style component for interactive confirm/question
// dialogs - e.g. those raised by plugins and tools via the host's
// Confirm/Input API. The caller is responsible for giving it exclusive input
// focus while it is visible and for removing it from the component tree once
// resolved (via OnConfirm/OnAnswer/OnCancel).
type Prompt struct {
	kind    PromptKind
	title   string
	message string

	input      *LineInput // question mode
	confirmYes bool       // confirm mode: which option is highlighted

	onConfirm func(confirmed bool)
	onAnswer  func(value string)
	onCancel  func()
```
