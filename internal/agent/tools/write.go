package tools

import (
	"context"
	"encoding/json"
	"errors"
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
			},
			"overwrite": {
				"type": "boolean",
				"description": "Allow overwriting an existing file. Defaults to false - the tool will error if the file already exists unless this is true."
			}
		},
		"required": ["path", "content"]
	}`),
}

// NewWriteTool creates the built-in write tool.
func NewWriteTool(cwd string, mq *MutationQueue, rt *ReadTracker) Tool {
	return newWriteTool(cwd, mq, rt, nil)
}

func newWriteTool(cwd string, mq *MutationQueue, rt *ReadTracker, grants *PathGrants) Tool {
	return Tool{
		Schema:  writeSchema,
		Source:  "builtin",
		Execute: makeWriteExecutor(cwd, mq, rt, grants),
	}
}

func makeWriteExecutor(cwd string, mq *MutationQueue, rt *ReadTracker, grants *PathGrants) Executor {
	return func(ctx context.Context, params json.RawMessage, ui UIBridge) (Result, error) {
		var p WriteParams
		if err := json.Unmarshal(params, &p); err != nil {
			return Result{Content: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
		}

		if len(p.Content) > maxWriteBytes {
			return Result{Content: fmt.Sprintf("content too large (%s > %s)", FormatSize(len(p.Content)), FormatSize(maxWriteBytes)), IsError: true}, nil
		}

		path := resolvePath(cwd, p.Path)

		if !grants.writable(ctx, ui, cwd, path) {
			return Result{Content: "path escapes working directory", IsError: true}, nil
		}

		writeCtx, cancel := context.WithTimeout(ctx, DefaultToolTimeout)
		defer cancel()

		result, err := runWrite(writeCtx, cwd, path, p, mq, rt)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return Result{Content: fmt.Sprintf("write timed out after %v", DefaultToolTimeout), IsError: true}, nil
			}
			return Result{Content: fmt.Sprintf("write failed: %v", err), IsError: true}, nil
		}

		return result, nil
	}
}

// runWrite performs the write under writeCtx, returning either the success
// Result or a nil error with the failure Result.
func runWrite(writeCtx context.Context, cwd, path string, p WriteParams, mq *MutationQueue, rt *ReadTracker) (Result, error) {
	var result Result
	err := runWithContext(writeCtx, func() error {
		oldContent, blocked := checkWriteGuards(cwd, path, p, rt)
		if blocked != nil {
			result = *blocked
			return nil
		}

		release := mq.Acquire(path)
		defer release()

		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			result = Result{Content: fmt.Sprintf("error creating directory: %v", err), IsError: true}
			return nil
		}

		if err := writeFileAtomic(path, []byte(p.Content), 0o644); err != nil {
			result = Result{Content: fmt.Sprintf("error writing file: %v", err), IsError: true}
			return nil
		}

		result = Result{
			Content: fmt.Sprintf("wrote %d bytes to %s", len(p.Content), path),
			Details: DiffDetails{Path: path, OldContent: oldContent, NewContent: p.Content},
		}
		return nil
	})
	return result, err
}

// checkWriteGuards enforces read-before-write and overwrite protection for an
// existing regular file. It returns the old content and, when a guard
// rejects the write, the failure Result to return.
func checkWriteGuards(cwd, path string, p WriteParams, rt *ReadTracker) (oldContent string, blocked *Result) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", nil // new files are always allowed
	}
	// Read-before-write: model must have read the file before mutating it.
	if rt != nil {
		if err := rt.CheckRead(cwd, p.Path); err != nil {
			return "", &Result{Content: err.Error(), IsError: true}
		}
	}
	// Overwrite protection: require explicit opt-in.
	if !p.Overwrite {
		return "", &Result{
			Content: fmt.Sprintf(
				"file %q already exists - set overwrite to true to replace it, or use the edit tool for partial changes",
				path,
			),
			IsError: true,
		}
	}
	if data, err := os.ReadFile(path); err == nil {
		oldContent = string(data)
	}
	return oldContent, nil
}
