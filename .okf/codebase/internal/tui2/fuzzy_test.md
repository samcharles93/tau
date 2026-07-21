---
description: Source module internal/tui2/fuzzy_test.go (292 lines).
resource: internal/tui2/fuzzy_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: fuzzy_test.go
type: Module
---

# Module fuzzy_test.go

**Path**: `internal/tui2/fuzzy_test.go`  
**Lines**: 292

## Snippet Preview

```
package tui2

import (
	"sort"
	"testing"
)

// --- FuzzyMatch ------------------------------------------------------------

func TestFuzzyMatchExact(t *testing.T) {
	r := FuzzyMatch("hello", "hello")
	if !r.Match {
		t.Fatal("exact match should match")
	}
	if r.Score >= 0 {
		t.Fatalf("exact match should have negative score (got %f)", r.Score)
	}
}

func TestFuzzyMatchCaseInsensitive(t *testing.T) {
	r := FuzzyMatch("HELLO", "hello")
	if !r.Match {
		t.Fatal("case-insensitive match should match")
	}

	r2 := FuzzyMatch("hello", "HELLO")
	if !r2.Match {
		t.Fatal("case-insensitive match should match")
	}
}
```
