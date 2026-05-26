package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReadParams are the parameters for the read tool.
type ReadParams struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"` // start line (1-based)
	Limit  int    `json:"limit,omitempty"`  // max lines to read
}

var readSchema = Schema{
	Name:        "read",
	Description: "Read file contents. Returns the file content with line numbers. Use offset and limit for large files.",
	Parameters: json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {
				"type": "string",
				"description": "Absolute or relative file path to read"
			},
			"offset": {
				"type": "integer",
				"description": "Start reading from this line number (1-based). Defaults to 1."
			},
			"limit": {
				"type": "integer",
				"description": "Maximum number of lines to read. Defaults to all."
			}
		},
		"required": ["path"]
	}`),
}

// NewReadTool creates the built-in read tool.
func NewReadTool(cwd string) Tool {
	return Tool{
		Schema:  readSchema,
		Source:  "builtin",
		Execute: makeReadExecutor(cwd),
	}
}

func makeReadExecutor(cwd string) Executor {
	return func(ctx context.Context, params json.RawMessage, _ UIBridge) (Result, error) {
		var p ReadParams
		if err := json.Unmarshal(params, &p); err != nil {
			return Result{Content: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
		}

		path := resolvePath(cwd, p.Path)

		data, err := os.ReadFile(path)
		if err != nil {
			return Result{Content: fmt.Sprintf("error reading file: %v", err), IsError: true}, nil
		}

		content := string(data)
		lines := strings.Split(content, "\n")

		// Apply offset (1-based).
		startLine := 1
		if p.Offset > 0 {
			startLine = p.Offset
		}
		if startLine > len(lines) {
			return Result{Content: fmt.Sprintf("offset %d exceeds file length (%d lines)", startLine, len(lines)), IsError: true}, nil
		}

		// Apply limit.
		endLine := len(lines)
		if p.Limit > 0 && startLine+p.Limit-1 < endLine {
			endLine = startLine + p.Limit - 1
		}

		// Build numbered output.
		var b strings.Builder
		for i := startLine - 1; i < endLine && i < len(lines); i++ {
			fmt.Fprintf(&b, "%4d │ %s\n", i+1, lines[i])
		}

		output := b.String()
		tr := TruncateHead(output, DefaultMaxLines, DefaultMaxBytes)

		return Result{Content: tr.Content}, nil
	}
}

// resolvePath resolves a potentially relative path against the working directory.
// It also strips a leading @ (some LLMs include this).
func resolvePath(cwd, path string) string {
	path = strings.TrimPrefix(path, "@")
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(cwd, path))
}
