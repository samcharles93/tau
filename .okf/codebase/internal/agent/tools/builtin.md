---
description: Source module internal/agent/tools/builtin.go (28 lines).
resource: internal/agent/tools/builtin.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: builtin.go
type: Module
---

# Module builtin.go

**Path**: `internal/agent/tools/builtin.go`  
**Lines**: 28

## Snippet Preview

```
package tools

// RegisterBuiltins registers all built-in tools into the given registry.
// The cwd parameter sets the working directory for file and shell operations.
// pluginDocs, if non-nil, is queried by the docs tool to merge in
// plugin-provided documentation; pass nil where no plugin manager exists.
func RegisterBuiltins(reg *Registry, cwd string, pluginDocs PluginDocsProvider, indexes ...GrepIndex) error {
	mq := NewMutationQueue()
	rt := NewReadTracker()

	builtins := []Tool{
		NewReadTool(cwd, rt),
		NewWriteTool(cwd, mq, rt),
		NewEditTool(cwd, mq, rt),
		NewShellTool(cwd, mq),
		NewGrepTool(cwd, indexes...),
		NewFindTool(cwd),
		NewDocsTool(pluginDocs),
	}

	for _, tool := range builtins {
		if err := reg.Register(tool); err != nil {
			return err
		}
	}

	return nil
}
```
