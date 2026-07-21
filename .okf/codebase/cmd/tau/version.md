---
description: Source module cmd/tau/version.go (38 lines).
resource: cmd/tau/version.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: version.go
type: Module
---

# Module version.go

**Path**: `cmd/tau/version.go`  
**Lines**: 38

## Snippet Preview

```
package main

import (
	"fmt"
	"time"
)

func formatVersion(version, date string, now time.Time) string {
	buildTime, err := time.Parse(time.RFC3339, date)
	if err != nil {
		return fmt.Sprintf("%s (built %s)", version, date)
	}

	return fmt.Sprintf(
		"%s (built %s, %s)",
		version,
		buildTime.UTC().Format(time.RFC3339),
		relativeAge(now, buildTime),
	)
}

func relativeAge(now, then time.Time) string {
	elapsed := now.Sub(then)
	switch {
	case elapsed < time.Minute:
		return "just now"
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm ago", int(elapsed/time.Minute))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(elapsed/time.Hour))
```
