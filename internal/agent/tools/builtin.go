package tools

// RegisterBuiltins registers all built-in tools into the given registry.
// The cwd parameter sets the working directory for file and shell operations.
func RegisterBuiltins(reg *Registry, cwd string) error {
	mq := NewMutationQueue()

	builtins := []Tool{
		NewReadTool(cwd),
		NewWriteTool(cwd, mq),
		NewEditTool(cwd, mq),
		NewShellTool(cwd),
		NewGrepTool(cwd),
		NewFindTool(cwd),
		NewLsTool(cwd),
	}

	for _, tool := range builtins {
		if err := reg.Register(tool); err != nil {
			return err
		}
	}

	return nil
}
