---
description: Source module internal/procid/procid_linux_test.go (60 lines).
resource: internal/procid/procid_linux_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: procid_linux_test.go
type: Module
---

# Module procid_linux_test.go

**Path**: `internal/procid/procid_linux_test.go`  
**Lines**: 60

## Snippet Preview

```
//go:build linux

package procid

import (
	"context"
	"os"
	"os/exec"
	"testing"
)

func TestCaptureProcessStartNS_SelfIsNonZero(t *testing.T) {
	ns := CaptureProcessStartNS(os.Getpid())
	if ns == 0 {
		t.Fatal("CaptureProcessStartNS(self) = 0, want a nonzero identity token")
	}
}

func TestCheckPIDIdentity_MatchingIdentityIsAlive(t *testing.T) {
	pid := os.Getpid()
	ns := CaptureProcessStartNS(pid)
	if ns == 0 {
		t.Skip("could not capture own process-start identity")
	}
	if got := CheckPIDIdentity(pid, ns); got != PIDCheckAlive {
		t.Errorf("CheckPIDIdentity(self, matching) = %v, want PIDCheckAlive", got)
	}
}

func TestCheckPIDIdentity_MismatchedIdentityIsDead(t *testing.T) {
```
