---
description: Source module pkg/taui/loader.go (137 lines).
resource: pkg/taui/loader.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: loader.go
type: Module
---

# Module loader.go

**Path**: `pkg/taui/loader.go`  
**Lines**: 137

## Snippet Preview

```
package taui

import (
	"sync"
	"time"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// Loader is an animated spinner component. Ported from Pi's components/loader.ts.
type Loader struct {
	Text
	baseMessage  string              // the message without spinner prefix
	spinnerFn    func(string) string // colour for the spinner frame
	msgFn        func(string) string // colour for the message
	frames       []string
	interval     time.Duration
	currentFrame int
	running      bool
	stopCh       chan struct{}
	onTick       func() // called after each frame to trigger re-render

	mu sync.Mutex // guards Text, baseMessage, frames, currentFrame, running, stopCh
}

// Default SpinnerFrames from termkit.
var DefaultFrames = termkit.SpinnerFrames

// NewLoader creates a spinner with the given message and colour callbacks.
func NewLoader(message string, spinnerFn, msgFn func(string) string) *Loader {
```
