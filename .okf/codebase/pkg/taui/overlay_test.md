---
description: Source module pkg/taui/overlay_test.go (108 lines).
resource: pkg/taui/overlay_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: overlay_test.go
type: Module
---

# Module overlay_test.go

**Path**: `pkg/taui/overlay_test.go`  
**Lines**: 108

## Snippet Preview

```
package taui

import "testing"

type fakeOverlay struct {
	handled     bool
	pasteOK     bool
	pasteCalled bool
	inputCalled bool
	lastInput   string
	lastPaste   string
}

func (f *fakeOverlay) Render(int) []string { return nil }
func (f *fakeOverlay) Invalidate()         {}
func (f *fakeOverlay) HandleInput(data string) bool {
	f.inputCalled = true
	f.lastInput = data
	return f.handled
}

type fakeOverlayWithPaste struct {
	fakeOverlay
}

func (f *fakeOverlayWithPaste) HandlePaste(content string) bool {
	f.pasteCalled = true
	f.lastPaste = content
	return f.pasteOK
}
```
