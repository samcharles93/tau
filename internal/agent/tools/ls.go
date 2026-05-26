package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LsParams are the parameters for the ls tool.
type LsParams struct {
	Path string `json:"path,omitempty"` // directory to list, defaults to cwd
}

var lsSchema = Schema{
	Name:        "ls",
	Description: "List directory contents. Shows files and subdirectories with type indicators (/ suffix for directories).",
	Parameters: json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {
				"type": "string",
				"description": "Directory path to list. Defaults to current directory."
			}
		}
	}`),
}

// NewLsTool creates the built-in ls tool.
func NewLsTool(cwd string) Tool {
	return Tool{
		Schema:  lsSchema,
		Source:  "builtin",
		Execute: makeLsExecutor(cwd),
	}
}

func makeLsExecutor(cwd string) Executor {
	return func(ctx context.Context, params json.RawMessage, _ UIBridge) (Result, error) {
		var p LsParams
		if err := json.Unmarshal(params, &p); err != nil {
			return Result{Content: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
		}

		dirPath := cwd
		if p.Path != "" {
			dirPath = resolvePath(cwd, p.Path)
		}

		entries, err := os.ReadDir(dirPath)
		if err != nil {
			return Result{Content: fmt.Sprintf("error listing directory: %v", err), IsError: true}, nil
		}

		// Separate dirs and files, sort each group.
		var dirs, files []string
		for _, entry := range entries {
			name := entry.Name()
			// Skip hidden files/dirs by default.
			if strings.HasPrefix(name, ".") {
				continue
			}
			if entry.IsDir() {
				dirs = append(dirs, name+"/")
			} else {
				files = append(files, name)
			}
		}
		sort.Strings(dirs)
		sort.Strings(files)

		var b strings.Builder
		relPath, _ := filepath.Rel(cwd, dirPath)
		if relPath == "" || relPath == "." {
			relPath = dirPath
		}
		fmt.Fprintf(&b, "%s\n", relPath)

		for _, d := range dirs {
			fmt.Fprintf(&b, "  %s\n", d)
		}
		for _, f := range files {
			fmt.Fprintf(&b, "  %s\n", f)
		}

		if len(dirs)+len(files) == 0 {
			b.WriteString("  (empty)\n")
		}

		return Result{Content: b.String()}, nil
	}
}
