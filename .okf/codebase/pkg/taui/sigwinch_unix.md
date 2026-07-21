---
description: Source module pkg/taui/sigwinch_unix.go (7 lines).
resource: pkg/taui/sigwinch_unix.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: sigwinch_unix.go
type: Module
---

# Module sigwinch_unix.go

**Path**: `pkg/taui/sigwinch_unix.go`  
**Lines**: 7

## Snippet Preview

```
//go:build unix

package taui

import "syscall"

func init() { sigWINCH = syscall.SIGWINCH }
```
