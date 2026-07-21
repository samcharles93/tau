---
description: Source module internal/cli/plugin_source_test.go (66 lines).
resource: internal/cli/plugin_source_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: plugin_source_test.go
type: Module
---

# Module plugin_source_test.go

**Path**: `internal/cli/plugin_source_test.go`  
**Lines**: 66

## Snippet Preview

```
package cli

import "testing"

func TestParsePluginSourceSpec(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    PluginSourceSpec
		wantErr bool
	}{
		{
			name: "valid without version",
			raw:  "samcharles93/tau-plugins:mcp",
			want: PluginSourceSpec{
				Owner:  "samcharles93",
				Repo:   "tau-plugins",
				Plugin: "mcp",
			},
		},
		{
			name: "valid with version",
			raw:  "samcharles93/tau-plugins:mcp@v1.2.3",
			want: PluginSourceSpec{
				Owner:   "samcharles93",
				Repo:    "tau-plugins",
				Plugin:  "mcp",
				Version: "v1.2.3",
			},
		},
```
