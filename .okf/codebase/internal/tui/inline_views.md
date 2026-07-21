---
description: Source module internal/tui/inline_views.go (193 lines).
resource: internal/tui/inline_views.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: inline_views.go
type: Module
---

# Module inline_views.go

**Path**: `internal/tui/inline_views.go`  
**Lines**: 193

## Snippet Preview

```
package tui

import (
	"strconv"
	"strings"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/pkg/taui"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// buildViewComponent renders a plugin-provided ExtensionView into a taui
// component tree: a padded Box containing an optional bold title followed by
// each widget, in order. Used for both the sync path (rendered once to
// scrollback) and the async path (mounted live into c.panels).
func buildViewComponent(view tauchat.ExtensionView) taui.Component {
	box := taui.NewBox().Padding(1, 0).Build()
	if view.Title != "" {
		titleFn := func(s string) string { return termkit.Wrap(s, termkit.Bold) }
		if fn := styleFn(view.Style); fn != nil {
			titleFn = func(s string) string { return fn(termkit.Wrap(s, termkit.Bold)) }
		}
		box.AddChild(taui.NewStyledText(view.Title, titleFn, nil))
	}
	for _, w := range view.Widgets {
		box.AddChild(renderWidget(w))
	}
	return box
}
```
