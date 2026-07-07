package tools

// RegisterBuiltins registers all built-in tools into the given registry.
// The cwd parameter sets the working directory for file and shell operations.
// pluginDocs, if non-nil, is queried by the docs tool to merge in
// plugin-provided documentation; pass nil where no plugin manager exists.
func RegisterBuiltins(reg *Registry, cwd string, pluginDocs PluginDocsProvider) error {
	mq := NewMutationQueue()
	rt := NewReadTracker()

	builtins := []Tool{
		NewReadTool(cwd, rt),
		NewWriteTool(cwd, mq, rt),
		NewEditTool(cwd, mq, rt),
		NewShellTool(cwd, mq),
		NewGrepTool(cwd),
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
