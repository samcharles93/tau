---
description: Source module internal/tui2/input_test.go (396 lines).
resource: internal/tui2/input_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: input_test.go
type: Module
---

# Module input_test.go

**Path**: `internal/tui2/input_test.go`  
**Lines**: 396

## Snippet Preview

```
package tui2

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

// key builds a tea.KeyPressMsg for a special key (Home, Left, ctrl+backspace,
// etc.) - no Text, matching what a real terminal sends for non-printable /
// modified keys (see the model_test.go sanity check in conversation history:
// setting Text on a Ctrl-modified letter suppresses the modifier in String()).
func key(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: mod}
}

// charKey builds a tea.KeyPressMsg for a plain printable character.
func charKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

func TestTypingInsertsAtCursorNotJustAppend(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	for _, r := range "helloworld" {
```
