---
description: Source module internal/tui2/textfield.go (105 lines).
resource: internal/tui2/textfield.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: textfield.go
type: Module
---

# Module textfield.go

**Path**: `internal/tui2/textfield.go`  
**Lines**: 105

## Snippet Preview

```
package tui2

import "unicode/utf8"

// textField is a standalone, single-line text-input widget. It owns its own
// rune buffer and cursor position rather than sharing model.input, so it can
// be reused anywhere a small piece of UI needs to collect free text from the
// user - both the agent-question flow (formPrompt, see prompt.go) and local
// flows like the GitHub Copilot Enterprise-domain prompt hold their own
// *textField instance.
type textField struct {
	value       string
	cursor      int // rune index into value, 0..len(runes)
	placeholder string
}

func newTextField(placeholder string) *textField {
	return &textField{placeholder: placeholder}
}

func (f *textField) Value() string { return f.value }

func (f *textField) SetValue(s string) {
	f.value = s
	f.cursor = utf8.RuneCountInString(s)
}

func (f *textField) Insert(s string) {
	if s == "" {
		return
```
