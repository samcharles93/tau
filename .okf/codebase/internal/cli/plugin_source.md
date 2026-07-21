---
description: Source module internal/cli/plugin_source.go (68 lines).
resource: internal/cli/plugin_source.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: plugin_source.go
type: Module
---

# Module plugin_source.go

**Path**: `internal/cli/plugin_source.go`  
**Lines**: 68

## Snippet Preview

```
package cli

import (
	"fmt"
	"strings"
)

// PluginSourceSpec represents a plugin install source in the form
// "owner/repo:plugin[@version]".
type PluginSourceSpec struct {
	Owner   string
	Repo    string
	Plugin  string
	Version string
}

// ParsePluginSourceSpec parses source specs such as:
//   - owner/repo:plugin
//   - owner/repo:plugin@v1.2.0
func ParsePluginSourceSpec(raw string) (PluginSourceSpec, error) {
	spec := strings.TrimSpace(raw)
	if spec == "" {
		return PluginSourceSpec{}, fmt.Errorf("plugin source cannot be empty")
	}

	parts := strings.Split(spec, ":")
	if len(parts) != 2 {
		return PluginSourceSpec{}, fmt.Errorf("invalid plugin source %q: expected owner/repo:plugin[@version]", raw)
	}

```
