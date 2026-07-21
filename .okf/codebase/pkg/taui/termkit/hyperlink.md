---
description: Source module pkg/taui/termkit/hyperlink.go (14 lines).
resource: pkg/taui/termkit/hyperlink.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: hyperlink.go
type: Module
---

# Module hyperlink.go

**Path**: `pkg/taui/termkit/hyperlink.go`  
**Lines**: 14

## Snippet Preview

```
package termkit

import "fmt"

// Hyperlink renders an OSC 8 terminal hyperlink: clickable text in supporting
// terminals (iTerm2, kitty, WezTerm, modern VTE), plain text everywhere else.
//
//	\033]8;;URL\033\\  TEXT  \033]8;;\033\\
func Hyperlink(label, url string) string {
	if !ColorEnabled() {
		return label
	}
	return fmt.Sprintf("\033]8;;%s\033\\%s\033]8;;\033\\", url, label)
}
```
