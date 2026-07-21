---
description: Source module internal/tui2/styles.go (167 lines).
resource: internal/tui2/styles.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: styles.go
type: Module
---

# Module styles.go

**Path**: `internal/tui2/styles.go`  
**Lines**: 167

## Snippet Preview

```
package tui2

import (
	"fmt"
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// themeHex converts a termkit.Color RGB triple to a lipgloss-compatible color.
func themeHex(c termkit.Color) color.Color {
	return lipgloss.Color(fmt.Sprintf("#%02X%02X%02X", c[0], c[1], c[2]))
}

var (
	// Semantic colors sourced from internal/theme.
	//
	// Chat message bodies (user/assistant/streaming) deliberately set no
	// Foreground at all: that lets the text inherit the terminal's own
	// default foreground instead of forcing a brand color across a large
	// block of content, matching Tau's rule of never overriding the user's
	// terminal theme. Only the small "⏎ " user-message glyph gets an
	// explicit accent, via userGlyphStyle below.
	inputColor = themeHex(theme.ShimmerHighlight)

	userGlyphStyle = lipgloss.NewStyle().Foreground(themeHex(theme.AccentColor)).Bold(true)
	userStyle      = lipgloss.NewStyle()
```
