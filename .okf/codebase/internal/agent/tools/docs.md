---
description: Source module internal/agent/tools/docs.go (170 lines).
resource: internal/agent/tools/docs.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: docs.go
type: Module
---

# Module docs.go

**Path**: `internal/agent/tools/docs.go`  
**Lines**: 170

## Snippet Preview

```
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/samcharles93/tau/docs"
)

// DocsParams are the parameters for the docs tool.
type DocsParams struct {
	Query string `json:"query,omitempty"`
	Path  string `json:"path,omitempty"`
}

var docsSchema = Schema{
	Name:        "docs",
	Description: "Access Tau's own documentation (user manual, configuration reference, developer guides). Provide 'query' to search all docs for a keyword or phrase, 'path' to read a full documentation file, or neither to list available files. Use when the user asks about Tau itself - usage, configuration, errors, skills, or capabilities.",
	Parameters: json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {
				"type": "string",
				"description": "Search term or phrase to locate across the documentation. Returns matching lines with file paths."
			},
			"path": {
```
