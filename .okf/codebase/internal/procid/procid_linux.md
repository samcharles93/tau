---
description: Source module internal/procid/procid_linux.go (88 lines).
resource: internal/procid/procid_linux.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: procid_linux.go
type: Module
---

# Module procid_linux.go

**Path**: `internal/procid/procid_linux.go`  
**Lines**: 88

## Snippet Preview

```
//go:build linux

package procid

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// linuxClockTicksPerSec is USER_HZ, the unit /proc/<pid>/stat's starttime
// field is reported in. 100 is standard on every mainstream Linux
// distribution/architecture (verified via sysconf(_SC_CLK_TCK) at build
// time on essentially all production kernels); reading it dynamically
// would require cgo, which this package avoids.
const linuxClockTicksPerSec = 100

// CaptureProcessStartNS reads pid's process-start time from
// /proc/<pid>/stat and converts it to nanoseconds since boot, to be
// persisted as agent_instances.process_start_ns. Returns 0 (unavailable)
// on any read/parse failure - callers treat 0 as "no identity recorded".
func CaptureProcessStartNS(pid int) int64 {
	ticks, err := readProcStartTicks(pid)
	if err != nil {
		return 0
	}
	return ticksToNS(ticks)
}
```
