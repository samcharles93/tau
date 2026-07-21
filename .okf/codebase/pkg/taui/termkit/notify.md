---
description: Source module pkg/taui/termkit/notify.go (111 lines).
resource: pkg/taui/termkit/notify.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: notify.go
type: Module
---

# Module notify.go

**Path**: `pkg/taui/termkit/notify.go`  
**Lines**: 111

## Snippet Preview

```
package termkit

import (
	"encoding/base64"
	"strings"
)

// osc99NotifyID is a fixed notification id so successive tau notifications
// replace the previous one (per the OSC 99 spec) rather than piling up in
// the terminal's/OS's notification center.
const osc99NotifyID = "tau-turn"

// notifyBodyMaxRunes bounds the notification body across every protocol
// below. Desktop notification UIs truncate long text anyway, and a smaller
// payload keeps each escape sequence itself small.
const notifyBodyMaxRunes = 300

// Notify returns a desktop-notification escape sequence carrying title and
// body, layering four protocols back to back so it degrades gracefully
// across terminals without sniffing $TERM/$TERM_PROGRAM (unreliable once a
// multiplexer is in the picture):
//
//   - OSC 99 (Kitty, Ghostty): rich notification, sent as two chunks sharing
//     one id - a bare OSC 99 payload is title-only, so title and body need
//     separate chunks per the spec.
//   - OSC 777 (rxvt-style; also honored by some other emulators): simpler
//     "notify;title;body" form with no escaping mechanism of its own, so
//     title/body are sanitized rather than base64-encoded.
//   - OSC 9 (iTerm2 and broad fallback): body-only, so title is folded in as
//     "title: body" when present.
```
