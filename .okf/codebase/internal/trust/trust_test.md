---
description: Source module internal/trust/trust_test.go (107 lines).
resource: internal/trust/trust_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: trust_test.go
type: Module
---

# Module trust_test.go

**Path**: `internal/trust/trust_test.go`  
**Lines**: 107

## Snippet Preview

```
package trust

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_MissingFileIsEmptyStore(t *testing.T) {
	s, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(s.TrustedSpecs) != 0 {
		t.Errorf("expected empty store, got %d entries", len(s.TrustedSpecs))
	}
	if s.IsTrusted("/some/project", "abc123") {
		t.Error("empty store must not trust anything")
	}
}

func TestTrustAndSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s.Trust("/proj", "hash1", ModeHash)
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
```
