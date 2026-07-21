---
description: Source module pkg/taui/termkit/notify_test.go (56 lines).
resource: pkg/taui/termkit/notify_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: notify_test.go
type: Module
---

# Module notify_test.go

**Path**: `pkg/taui/termkit/notify_test.go`  
**Lines**: 56

## Snippet Preview

```
package termkit

import (
	"strings"
	"testing"
)

func TestNotify_containsAllProtocols(t *testing.T) {
	got := Notify("tau", "response ready")

	if !strings.Contains(got, "\x1b]99;i=tau-turn:d=0:e=1;") {
		t.Errorf("Notify() missing OSC 99 title chunk: %q", got)
	}
	if !strings.Contains(got, "\x1b]99;i=tau-turn:d=1:e=1:p=body;") {
		t.Errorf("Notify() missing OSC 99 body chunk: %q", got)
	}
	if !strings.Contains(got, "\x1b]777;notify;tau;response ready\x07") {
		t.Errorf("Notify() missing OSC 777 sequence: %q", got)
	}
	if !strings.Contains(got, "\x1b]9;tau: response ready\x07") {
		t.Errorf("Notify() missing OSC 9 sequence: %q", got)
	}
	if !strings.HasSuffix(got, "\a") {
		t.Errorf("Notify() should end with a bell fallback: %q", got)
	}
}

func TestNotify_sanitizesSemicolonsForOSC777(t *testing.T) {
	got := Notify("a;b", "c;d")
	if !strings.Contains(got, "\x1b]777;notify;a,b;c,d\x07") {
```
