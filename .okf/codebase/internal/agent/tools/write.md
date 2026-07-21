---
description: Source module internal/agent/tools/write.go (113 lines).
resource: internal/agent/tools/write.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: write.go
type: Module
---

# Module write.go

**Path**: `internal/agent/tools/write.go`  
**Lines**: 113

## Snippet Preview

```
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WriteParams are the parameters for the write tool.
type WriteParams struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Overwrite bool   `json:"overwrite,omitempty"`
}

var writeSchema = Schema{
	Name:        "write",
	Description: "Create or overwrite a file with the given content. Creates parent directories as needed. Set overwrite to true to replace an existing file; otherwise the tool will refuse to overwrite.",
	Parameters: json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {
				"type": "string",
				"description": "Absolute or relative file path to write"
			},
			"content": {
				"type": "string",
				"description": "The full file content to write"
```
