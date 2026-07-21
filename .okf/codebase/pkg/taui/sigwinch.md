---
description: Source module pkg/taui/sigwinch.go (7 lines).
resource: pkg/taui/sigwinch.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: sigwinch.go
type: Module
---

# Module sigwinch.go

**Path**: `pkg/taui/sigwinch.go`  
**Lines**: 7

## Snippet Preview

```
package taui

import "os"

// sigWINCH is nil on platforms without SIGWINCH support (e.g. Windows).
// Set by platform-specific init() functions on Unix systems.
var sigWINCH os.Signal
```
