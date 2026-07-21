---
description: Source module pkg/taui/box.go (189 lines).
resource: pkg/taui/box.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: box.go
type: Module
---

# Module box.go

**Path**: `pkg/taui/box.go`  
**Lines**: 189

## Snippet Preview

```
package taui

import (
	"strings"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// BgFn is a callback that applies a background colour to a string.
type BgFn func(text string) string

// FgFn is a callback that applies a foreground colour to a string.
type FgFn func(text string) string

// Box is a container that applies padding and background colour to its
// children. Build it with the BoxBuilder:
//
//	box := NewBox().
//	    Padding(2, 1).
//	    Bg(amberBg).
//	    ExpandW().    // fill terminal width
//	    ExpandH().    // fill terminal height
//	    Build()
type Box struct {
	Container
	padX    int
	padY    int
	bgFn    BgFn
	expandW bool // pad to full terminal width
	expandH bool // fill remaining terminal height
```
