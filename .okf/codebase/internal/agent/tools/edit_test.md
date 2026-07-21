---
description: Source module internal/agent/tools/edit_test.go (261 lines).
resource: internal/agent/tools/edit_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: edit_test.go
type: Module
---

# Module edit_test.go

**Path**: `internal/agent/tools/edit_test.go`  
**Lines**: 261

## Snippet Preview

```
package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyEdits(t *testing.T) {
	cases := []struct {
		name    string
		content string
		edits   []EditAction
		want    string
		wantErr string
	}{
		{
			name:    "single edit",
			content: "func a() {}\nfunc b() {}\n",
			edits:   []EditAction{{OldText: "func a()", NewText: "func alpha()"}},
			want:    "func alpha() {}\nfunc b() {}\n",
		},
		{
			name:    "multiple disjoint edits applied against original",
			content: "one\ntwo\nthree\n",
			edits: []EditAction{
				{OldText: "three", NewText: "3"},
```
