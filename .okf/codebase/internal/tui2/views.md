---
description: Source module internal/tui2/views.go (273 lines).
resource: internal/tui2/views.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: views.go
type: Module
---

# Module views.go

**Path**: `internal/tui2/views.go`  
**Lines**: 273

## Snippet Preview

```
package tui2

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// renderPluginView renders a plugin-provided ExtensionView's widgets into a
// plain string block - the lipgloss/v2 equivalent of
// internal/tui/inline_views.go's buildViewComponent. The view's own Title is
// rendered separately by the caller (tui2 shows it in the panel border), so
// only the widget bodies are joined here.
func renderPluginView(view tauchat.ExtensionView) string {
	parts := make([]string, 0, len(view.Widgets))
	for _, w := range view.Widgets {
		parts = append(parts, renderWidget(w))
	}
	return strings.Join(parts, "\n")
}

// renderWidget converts one domain Widget into its rendered string. An
// unrecognized/zero-value Widget (e.g. a newer kind sent by a plugin this
// host doesn't understand yet) renders empty - the same additive-evolution
```
