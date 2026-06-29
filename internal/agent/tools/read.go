package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
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
func NewReadTool(cwd string, rt *ReadTracker) Tool {
	return Tool{
		Schema:  readSchema,
		Source:  "builtin",
		Execute: makeReadExecutor(cwd, rt),
	}
}

func makeReadExecutor(cwd string, rt *ReadTracker) Executor {
	return func(ctx context.Context, params json.RawMessage, _ UIBridge) (Result, error) {
		var p ReadParams
		if err := json.Unmarshal(params, &p); err != nil {
			return Result{Content: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
		}

		if p.Offset < 0 {
			return Result{Content: "offset must be >= 0", IsError: true}, nil
		}
		if p.Limit < 0 {
			return Result{Content: "limit must be >= 0", IsError: true}, nil
		}

		ctx, cancel := context.WithTimeout(ctx, DefaultToolTimeout)
		defer cancel()

		path := resolvePath(cwd, p.Path)

		if !isConfined(cwd, path) {
			return Result{Content: "path escapes working directory", IsError: true}, nil
		}

		info, err := os.Stat(path)
		if err != nil {
			return Result{Content: fmt.Sprintf("error stating file: %v", err), IsError: true}, nil
		}
		if info.IsDir() {
			return Result{Content: "path is a directory, not a file", IsError: true}, nil
		}
		if info.Size() > maxReadBytes {
			return Result{Content: fmt.Sprintf("file too large (%s > %s)", FormatSize(int(info.Size())), FormatSize(maxReadBytes)), IsError: true}, nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return Result{Content: fmt.Sprintf("error reading file: %v", err), IsError: true}, nil
		}

		// Record the read so mutation tools can enforce read-before-write.
		// Only record after a successful ReadFile so we don't mark files
		// that couldn't actually be read.
		if rt != nil {
			rt.MarkRead(cwd, p.Path)
		}

		if !utf8.Valid(data) {
			return Result{Content: "file appears to be binary", IsError: true}, nil
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
