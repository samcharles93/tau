---
description: Source module internal/tui/statusbar_test.go (300 lines).
resource: internal/tui/statusbar_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: statusbar_test.go
type: Module
---

# Module statusbar_test.go

**Path**: `internal/tui/statusbar_test.go`  
**Lines**: 300

## Snippet Preview

```
package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/rivo/uniseg"

	"github.com/samcharles93/tau/pkg/taui"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// sgrSeq matches complete ANSI SGR escape sequences (the only kind the status
// bar emits). Stripping these and then checking for a stray ESC byte detects a
// severed/partial escape, which is the signature of ANSI-unaware truncation.
var sgrSeq = regexp.MustCompile("\x1b\\[[0-9;]*m")

// assertNoSeveredANSI fails if out contains an ESC byte that is not part of a
// complete SGR sequence (i.e. a truncation cut through an escape).
func assertNoSeveredANSI(t *testing.T, out string) {
	t.Helper()
	if rest := sgrSeq.ReplaceAllString(out, ""); strings.ContainsRune(rest, '\x1b') {
		t.Fatalf("severed/partial ANSI escape leaked: %q", out)
	}
}

// plainWidth measures a right-group subset the way renderStatusBar does, so tests
// can derive exact width thresholds instead of hard-coding cell counts.
func plainWidth(segs []statusSeg) int {
```
