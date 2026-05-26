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
	Path    string `json:"path"`
	Content string `json:"content"`
}

var writeSchema = Schema{
	Name:        "write",
	Description: "Create or overwrite a file with the given content. Creates parent directories as needed.",
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
			}
		},
		"required": ["path", "content"]
	}`),
}

// NewWriteTool creates the built-in write tool.
func NewWriteTool(cwd string, mq *MutationQueue) Tool {
	return Tool{
		Schema:  writeSchema,
		Source:  "builtin",
		Execute: makeWriteExecutor(cwd, mq),
	}
}

func makeWriteExecutor(cwd string, mq *MutationQueue) Executor {
	return func(ctx context.Context, params json.RawMessage, _ UIBridge) (Result, error) {
		var p WriteParams
		if err := json.Unmarshal(params, &p); err != nil {
			return Result{Content: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
		}

		if len(p.Content) > maxWriteBytes {
			return Result{Content: fmt.Sprintf("content too large (%s > %s)", FormatSize(len(p.Content)), FormatSize(maxWriteBytes)), IsError: true}, nil
		}

		path := resolvePath(cwd, p.Path)

		if !isConfined(cwd, path) {
			return Result{Content: "path escapes working directory", IsError: true}, nil
		}

		release := mq.Acquire(path)
		defer release()

		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Result{Content: fmt.Sprintf("error creating directory: %v", err), IsError: true}, nil
		}

		if err := writeFileAtomic(path, []byte(p.Content), 0o644); err != nil {
			return Result{Content: fmt.Sprintf("error writing file: %v", err), IsError: true}, nil
		}

		return Result{Content: fmt.Sprintf("wrote %d bytes to %s", len(p.Content), path)}, nil
	}
}
