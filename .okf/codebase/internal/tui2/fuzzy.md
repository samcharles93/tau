---
description: Source module internal/tui2/fuzzy.go (147 lines).
resource: internal/tui2/fuzzy.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: fuzzy.go
type: Module
---

# Module fuzzy.go

**Path**: `internal/tui2/fuzzy.go`  
**Lines**: 147

## Snippet Preview

```
package tui2

import (
	"regexp"
	"strings"
)

// Fuzzy matching, ported from pkg/taui/fuzzy.go (itself ported from Pi's
// fuzzy.ts, MIT, © 2025 Mario Zechner) so tui2's completions dropdown scores,
// ranks, and highlights matches identically to the legacy taui frontend.
// A query matches if all its characters appear in order within the candidate
// (not necessarily consecutively). Lower score = better match.

// FuzzyResult is the outcome of matching a query against a candidate string.
type FuzzyResult struct {
	Match     bool
	Score     float64 // lower is better
	Positions []int   // rune indices in the candidate that matched the query
}

var (
	fuzzyLettersDigits = regexp.MustCompile(`^([a-z]+)([0-9]+)$`)
	fuzzyDigitsLetters = regexp.MustCompile(`^([0-9]+)([a-z]+)$`)
)

func isFuzzyBoundary(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '-', '_', '.', '/', ':':
		return true
	}
```
