---
description: Source module internal/tui/inline_chat_test.go (493 lines).
resource: internal/tui/inline_chat_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: inline_chat_test.go
type: Module
---

# Module inline_chat_test.go

**Path**: `internal/tui/inline_chat_test.go`  
**Lines**: 493

## Snippet Preview

```
package tui

import (
	"context"
	"sync"
	"testing"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/internal/tui/notify"
	"github.com/samcharles93/tau/pkg/taui"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// nullTerminal is a no-op Terminal so the engine can render without touching a
// real tty during tests.
type nullTerminal struct{}

func (nullTerminal) Start(func(string), func()) {}
func (nullTerminal) SignalStop()                {}
func (nullTerminal) Stop()                      {}
func (nullTerminal) Write(string)               {}
func (nullTerminal) Size() (int, int)           { return 80, 24 }
func (nullTerminal) HideCursor()                {}
func (nullTerminal) ShowCursor()                {}
func (nullTerminal) MoveBy(int)                 {}
func (nullTerminal) ClearLine()                 {}
func (nullTerminal) ClearToEnd()                {}
func (nullTerminal) ClearScreen()               {}
```
