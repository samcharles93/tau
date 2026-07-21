---
description: Source module internal/tui2/views_test.go (288 lines).
resource: internal/tui2/views_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: views_test.go
type: Module
---

# Module views_test.go

**Path**: `internal/tui2/views_test.go`  
**Lines**: 288

## Snippet Preview

```
package tui2

import (
	"strings"
	"testing"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

// --- renderWidget: text ------------------------------------------------------

func TestRenderWidgetTextPlain(t *testing.T) {
	w := tauchat.Widget{Kind: tauchat.WidgetKindText, Text: &tauchat.TextWidget{Text: "hello"}}
	if got := stripANSI(renderWidget(w)); got != "hello" {
		t.Fatalf("got %q, want %q", got, "hello")
	}
}

func TestRenderWidgetTextNilPayload(t *testing.T) {
	w := tauchat.Widget{Kind: tauchat.WidgetKindText}
	if got := renderWidget(w); got != "" {
		t.Fatalf("expected empty string for nil Text payload, got %q", got)
	}
}

func TestRenderWidgetUnknownKind(t *testing.T) {
	w := tauchat.Widget{Kind: "some-future-kind"}
	if got := renderWidget(w); got != "" {
		t.Fatalf("expected empty string for an unrecognized kind, got %q", got)
	}
```
