---
description: Source module internal/agent/tools/registry.go (290 lines).
resource: internal/agent/tools/registry.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: registry.go
type: Module
---

# Module registry.go

**Path**: `internal/agent/tools/registry.go`  
**Lines**: 290

## Snippet Preview

```
// Package tools provides the tool registry and built-in tool implementations
// for the Tau agent coordinator.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Schema describes a tool's interface for LLM function-calling.
type Schema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"` // JSON Schema object
}

// Result is the output of a tool execution.
type Result struct {
	Content   string `json:"content"`
	Details   any    `json:"details,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
	ErrorKind string `json:"error_kind,omitempty"`
	// MetricLabels adds tool-specific low-cardinality dimensions to the
	// coordinator's authoritative completion metric.
	MetricLabels map[string]string `json:"-"`
```
