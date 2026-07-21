---
description: Source module pkg/taui/termkit/clipboard.go (26 lines).
resource: pkg/taui/termkit/clipboard.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: clipboard.go
type: Module
---

# Module clipboard.go

**Path**: `pkg/taui/termkit/clipboard.go`  
**Lines**: 26

## Snippet Preview

```
package termkit

import "encoding/base64"

// OSC52MaxBytes caps the size of text passed to OSC52Copy. The payload rides
// inside a single terminal control sequence, and most emulators cap how much
// of one they'll buffer (kitty is generous; xterm and others sit in the tens
// of kilobytes) - bytes past that limit are silently dropped rather than
// truncated-and-copied, so it's better to refuse up front than hand the user
// a corrupted clipboard.
const OSC52MaxBytes = 100_000

// OSC52Copy returns an OSC 52 escape sequence that asks the terminal to set
// the system clipboard to text, and reports whether text was small enough to
// send (see OSC52MaxBytes). Supported by kitty, iTerm2, WezTerm, foot,
// alacritty, and (with `set-clipboard on`) tmux - notably including over SSH,
// where no other mechanism can reach the local clipboard. The protocol has no
// acknowledgement, so a caller can't detect whether an unsupporting terminal
// silently ignored it.
func OSC52Copy(text string) (seq string, ok bool) {
	if len(text) > OSC52MaxBytes {
		return "", false
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	return "\x1b]52;c;" + encoded + "\x07", true
}
```
