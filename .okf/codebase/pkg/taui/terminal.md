---
description: Source module pkg/taui/terminal.go (236 lines).
resource: pkg/taui/terminal.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: terminal.go
type: Module
---

# Module terminal.go

**Path**: `pkg/taui/terminal.go`  
**Lines**: 236

## Snippet Preview

```
package taui

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// Terminal abstracts the terminal device. Ported from Pi's terminal.ts.
type Terminal interface {
	// Start begins listening for input and resize events.
	Start(onInput func(data string), onResize func())

	// Stop restores terminal state.
	Stop()

	// SignalStop closes the stop channel to request the stdin and resize
	// goroutines exit, without waiting for them. It is safe to call multiple
	// times and from any goroutine. Call Stop afterwards to wait for the
	// goroutines and restore terminal state.
	SignalStop()

	// Write outputs data to the terminal.
	Write(data string)

```
