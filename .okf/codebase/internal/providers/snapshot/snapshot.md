---
description: Source module internal/providers/snapshot/snapshot.go (30 lines).
resource: internal/providers/snapshot/snapshot.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: snapshot.go
type: Module
---

# Module snapshot.go

**Path**: `internal/providers/snapshot/snapshot.go`  
**Lines**: 30

## Snippet Preview

```
// Package snapshot provides tau's embedded provider+model catalogue: a curated,
// filtered copy of models.dev baked into the binary so model discovery works
// offline and is restricted to providers tau knows about and models that can
// call tools. Regenerate with `go generate ./internal/providers/snapshot`.
package snapshot

import (
	_ "embed"
	"fmt"

	"github.com/samcharles93/ai-sdk/runtime"
)

//go:generate go run ./gen

//go:embed models.json
var data []byte

// Data returns the raw embedded snapshot JSON in models.dev format.
func Data() []byte { return data }

// Catalog returns an ai-sdk catalogue preloaded from the embedded snapshot, with
// no network access. Callers pass it to runtime.NewRuntimeWithCatalog.
func Catalog() (*runtime.Catalog, error) {
	cat := runtime.NewCatalog(runtime.CatalogOptions{})
	if err := cat.LoadFromJSON(data); err != nil {
		return nil, fmt.Errorf("load embedded model snapshot: %w", err)
	}
	return cat, nil
}
```
