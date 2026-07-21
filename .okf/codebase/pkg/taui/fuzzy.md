---
description: Source module pkg/taui/fuzzy.go (117 lines).
resource: pkg/taui/fuzzy.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: fuzzy.go
type: Module
---

# Module fuzzy.go

**Path**: `pkg/taui/fuzzy.go`  
**Lines**: 117

## Snippet Preview

```
package taui

import (
	"regexp"
	"strings"
)

// Fuzzy matching, ported from Pi's fuzzy.ts (MIT, © 2025 Mario Zechner).
// A query matches if all its characters appear in order within the candidate
// (not necessarily consecutively). Lower score = better match. The matched rune
// positions are tracked so callers can highlight them.

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
	return false
```
