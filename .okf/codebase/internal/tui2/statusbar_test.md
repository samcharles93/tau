---
description: Source module internal/tui2/statusbar_test.go (683 lines).
resource: internal/tui2/statusbar_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: statusbar_test.go
type: Module
---

# Module statusbar_test.go

**Path**: `internal/tui2/statusbar_test.go`  
**Lines**: 683

## Snippet Preview

```
package tui2

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/metrics"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// --- renderStatusBar --------------------------------------------------------

func TestRenderStatusBarBasic(t *testing.T) {
	left := []statusSeg{{text: "tau"}, {text: "gpt-4"}}
	right := []statusSeg{{text: "1.2k tok", prio: prioTokens}}

	out := renderStatusBar(80, left, right)
	if out == "" {
		t.Fatal("expected non-empty status bar")
	}
	plain := stripANSI(out)
	if !strings.Contains(plain, "tau") {
		t.Errorf("status bar should contain 'tau', got %q", plain)
	}
	if !strings.Contains(plain, "gpt-4") {
```
