---
description: Source module cmd/tau/main_test.go (74 lines).
resource: cmd/tau/main_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: main_test.go
type: Module
---

# Module main_test.go

**Path**: `cmd/tau/main_test.go`  
**Lines**: 74

## Snippet Preview

```
package main

import (
	"testing"
	"time"
)

func TestFormatVersion(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 14, 8, 32, 7, 0, time.UTC)
	tests := []struct {
		name    string
		version string
		date    string
		want    string
	}{
		{
			name:    "release build",
			version: "0.18.1",
			date:    "2026-07-12T07:32:07Z",
			want:    "0.18.1 (built 2026-07-12T07:32:07Z, 2d ago)",
		},
		{
			name:    "normalizes timestamp to UTC",
			version: "0.18.1",
			date:    "2026-07-14T17:02:07+10:00",
			want:    "0.18.1 (built 2026-07-14T07:02:07Z, 1h ago)",
		},
		{
```
