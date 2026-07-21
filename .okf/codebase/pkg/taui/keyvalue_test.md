---
description: Source module pkg/taui/keyvalue_test.go (38 lines).
resource: pkg/taui/keyvalue_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: keyvalue_test.go
type: Module
---

# Module keyvalue_test.go

**Path**: `pkg/taui/keyvalue_test.go`  
**Lines**: 38

## Snippet Preview

```
package taui

import "testing"

func TestKeyValueAlignsKeys(t *testing.T) {
	kv := NewKeyValue([]KeyValueEntry{
		{Key: "a", Value: "1"},
		{Key: "longer", Value: "2"},
	})
	lines := kv.Render(80)
	want := []string{"a     : 1", "longer: 2"}
	if len(lines) != len(want) {
		t.Fatalf("got %d lines, want %d: %v", len(lines), len(want), lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestKeyValueEmpty(t *testing.T) {
	kv := NewKeyValue(nil)
	if lines := kv.Render(80); lines != nil {
		t.Errorf("expected nil lines for empty KeyValue, got %v", lines)
	}
}

func TestKeyValueTruncatesToWidth(t *testing.T) {
	kv := NewKeyValue([]KeyValueEntry{{Key: "k", Value: "a very long value that exceeds the width"}})
```
