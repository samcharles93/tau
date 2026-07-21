---
description: Source module pkg/taui/lineinput.go (688 lines).
resource: pkg/taui/lineinput.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: lineinput.go
type: Module
---

# Module lineinput.go

**Path**: `pkg/taui/lineinput.go`  
**Lines**: 688

## Snippet Preview

```
package taui

import (
	"fmt"
	"strings"
	"sync"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// LineInput is a multi-line text input with a visible block cursor and the
// common readline-style editing keys. Shift+Enter or Ctrl+J inserts a newline;
// Enter submits. It is Focusable and implements PasteHandler. Slimmed port of
// Pi's editor-component.ts for inline prompts.
type LineInput struct {
	mu       sync.Mutex
	runes    []rune
	cursor   int // index into runes, 0..len(runes)
	prompt   string
	hint     string // placeholder shown when empty
	focused  bool
	onSubmit func(string)

	promptFn func(string) string
	textFn   func(string) string
	hintFn   func(string) string
	cmdFn    func(string) string // applied instead of textFn while the line begins with "/"

	cursorR, cursorG, cursorB uint8          // cursor background colour (0 = default grey)
	cmdCursor                 *termkit.Color // cursor background used instead while isCommand (see SetCommandCursorColor)
```
