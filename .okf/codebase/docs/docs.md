---
description: Source module docs/docs.go (8 lines).
resource: docs/docs.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: docs.go
type: Module
---

# Module docs.go

**Path**: `docs/docs.go`  
**Lines**: 8

## Snippet Preview

```
package docs

import (
	"embed"
)

//go:embed *.md *.yaml examples specs
var FS embed.FS
```
