---
description: Source module pkg/taui/kitty_test.go (91 lines).
resource: pkg/taui/kitty_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: kitty_test.go
type: Module
---

# Module kitty_test.go

**Path**: `pkg/taui/kitty_test.go`  
**Lines**: 91

## Snippet Preview

```
package taui

import "testing"

func TestCanonicalKeyNormalizesDisambiguated(t *testing.T) {
	cases := map[string]string{
		"\x1b[27u":    "\x1b", // Esc
		"\x1b[13u":    "\r",   // Enter
		"\x1b[9u":     "\t",   // Tab
		"\x1b[127u":   "\x7f", // Backspace
		"\x1b[99;5u":  "\x03", // Ctrl+C (Kitty CSI-u, codepoint form)
		"\x1b[3;5u":   "\x03", // Ctrl+C (Kitty CSI-u, control-char form)
		"\x1b[106;5u": "\x0a", // Ctrl+J - newline
		"\x1b[10;5u":  "\x0a", // Ctrl+J (control-char form)
		"\x1b[115;5u": "\x13", // Ctrl+S - steer
		"\x1b[97;5u":  "\x01", // Ctrl+A - home
		"\x1b[101;5u": "\x05", // Ctrl+E - end
		"\x1b[119;5u": "\x17", // Ctrl+W - delete word

		// Real ghostty sequences with NumLock active (modifier 133 = 1+Ctrl+NumLock):
		// the lock bit must be ignored so these still resolve to control bytes.
		"\x1b[99;133u":  "\x03", // Ctrl+C
		"\x1b[106;133u": "\x0a", // Ctrl+J
		"\x1b[115;133u": "\x13", // Ctrl+S
		"\x1b[97;133u":  "\x01", // Ctrl+A
		// Event-type sub-parameter (press/repeat/release) must be ignored too.
		"\x1b[99;5:1u": "\x03", // Ctrl+C, press event
	}
	for in, want := range cases {
		if got := canonicalKey(in); got != want {
```
