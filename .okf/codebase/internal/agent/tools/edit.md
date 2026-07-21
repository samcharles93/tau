---
description: Source module internal/agent/tools/edit.go (360 lines).
resource: internal/agent/tools/edit.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: edit.go
type: Module
---

# Module edit.go

**Path**: `internal/agent/tools/edit.go`  
**Lines**: 360

## Snippet Preview

```
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// EditParams are the parameters for the edit tool.
type EditParams struct {
	Path  string       `json:"path"`
	Edits []EditAction `json:"edits"`
}

// EditAction is a single search-and-replace operation.
type EditAction struct {
	OldText    string `json:"old_text"`
	NewText    string `json:"new_text"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

var editSchema = Schema{
	Name:        "edit",
	Description: "Edit an existing file using exact text replacement. Each edit specifies old_text (must match exactly including whitespace) and new_text. Every old_text is matched against the original file and must be unique in it; set replace_all to true to replace every occurrence instead. Edits must not overlap - merge nearby changes into one edit. All edits are validated first and applied atomically: on any failure the file is left unchanged.",
	Parameters: json.RawMessage(`{
		"type": "object",
		"properties": {
```
