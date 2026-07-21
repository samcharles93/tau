---
description: Source module pkg/taui/termkit/termkit_test.go (105 lines).
resource: pkg/taui/termkit/termkit_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: termkit_test.go
type: Module
---

# Module termkit_test.go

**Path**: `pkg/taui/termkit/termkit_test.go`  
**Lines**: 105

## Snippet Preview

```
package termkit

import (
	"strings"
	"testing"
)

func TestColorEnabled_noColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	// Reset the cached detection so the env var takes effect.
	colorMu.Lock()
	colorChecked = false
	colorMu.Unlock()

	if ColorEnabled() {
		t.Error("ColorEnabled() should be false when NO_COLOR is set")
	}
}

func TestStyle_colorDisabled(t *testing.T) {
	DisableColor()
	got := Style("hello", Bold, ColorRed.Fg())
	if got != "hello" {
		t.Errorf("Style() with color disabled should return plain text, got %q", got)
	}
}

func TestStyle_colorEnabled(t *testing.T) {
	ForceColor()

```
