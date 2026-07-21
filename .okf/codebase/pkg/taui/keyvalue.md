---
description: Source module pkg/taui/keyvalue.go (57 lines).
resource: pkg/taui/keyvalue.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: keyvalue.go
type: Module
---

# Module keyvalue.go

**Path**: `pkg/taui/keyvalue.go`  
**Lines**: 57

## Snippet Preview

```
package taui

import (
	"strings"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// KeyValueEntry is one row of a KeyValue component.
type KeyValueEntry struct {
	Key   string
	Value string
	// ValueFn optionally styles the value, applied after column alignment so
	// padding is computed from the unstyled visible width.
	ValueFn func(string) string
}

// KeyValue renders aligned "key: value" lines, with keys padded to the width
// of the longest key.
type KeyValue struct {
	entries []KeyValueEntry
}

// NewKeyValue creates a KeyValue component.
func NewKeyValue(entries []KeyValueEntry) *KeyValue {
	return &KeyValue{entries: entries}
}

// Invalidate is a no-op; KeyValue holds no cached render.
func (kv *KeyValue) Invalidate() {}
```
