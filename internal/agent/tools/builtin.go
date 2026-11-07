package tools

// RegisterBuiltins registers all built-in tools into the given registry.
// The cwd parameter sets the working directory for file and shell operations.
// pluginDocs, if non-nil, is queried by the docs tool to merge in
// plugin-provided documentation; pass nil where no plugin manager exists.
func RegisterBuiltins(reg *Registry, cwd string, pluginDocs PluginDocsProvider, indexes ...GrepIndex) error {
	mq := NewMutationQueue()
	rt := NewReadTracker()
	grants := NewPathGrants()

	builtins := []Tool{
		newReadTool(cwd, rt, grants),
		newWriteTool(cwd, mq, rt, grants),
		newEditTool(cwd, mq, rt, grants),
		NewShellTool(cwd, mq),
		newGrepTool(cwd, grants, indexes...),
		newFindTool(cwd, grants),
		NewDocsTool(pluginDocs),
	}

	for _, tool := range builtins {
		if err := reg.Register(tool); err != nil {
			return err
		}
	}

	return nil
}
