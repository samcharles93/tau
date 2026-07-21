---
description: Source module internal/procid/procid_windows.go (48 lines).
resource: internal/procid/procid_windows.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: procid_windows.go
type: Module
---

# Module procid_windows.go

**Path**: `internal/procid/procid_windows.go`  
**Lines**: 48

## Snippet Preview

```
//go:build windows

package procid

import (
	"log/slog"

	"golang.org/x/sys/windows"
)

// stillActive is the STILL_ACTIVE sentinel (259) GetExitCodeProcess
// returns for a running process. Not exported by golang.org/x/sys/windows.
const stillActive = 259

// CaptureProcessStartNS has no implementation on this platform (see
// docs/specs/agents/04-storage-and-sessions.md, Orphan sweep: the Windows
// GetProcessTimes path is documented but not implemented here - it can't
// be verified without a Windows build/test target, and a wrong syscall
// implementation is worse than an honest PID-only fallback). Returns 0
// (unavailable); the sweep falls back to PID-only + the stale-age bound.
func CaptureProcessStartNS(_ int) int64 {
	return 0
}

// CheckPIDIdentity falls back to a PID-only liveness check (open the
// process handle; ERROR_INVALID_PARAMETER means no such pid), per the
// spec's documented "Unsupported platform" behavior. wantStartNS is
// ignored since no identity was ever captured on this platform.
func CheckPIDIdentity(pid int, _ int64) PIDCheck {
	slog.Warn("orphan sweep: process-start identity unsupported on this platform, falling back to PID-only check", "pid", pid)
```
