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

// GlobParams are the parameters for the glob tool.
type GlobParams struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"` // directory to search in, defaults to cwd
}

var globSchema = Schema{
	Name:        "glob",
	Description: "Find files matching a glob pattern using pure-Go matching. Fast for simple patterns without requiring external binaries. Use for patterns like '**/*.go', '*.md', 'internal/**/tools/*.go'.",
	Parameters: json.RawMessage(`{
		"type": "object",
		"properties": {
			"pattern": {
				"type": "string",
				"description": "Glob pattern to match file paths against (e.g. '**/*.go', '*.md', 'internal/**/tools/*.go')"
			},
			"path": {
				"type": "string",
				"description": "Directory to search in. Defaults to current directory."
			}
		},
		"required": ["pattern"]
	}`),
}

// NewGlobTool creates the built-in glob tool.
func NewGlobTool(cwd string) Tool {
	return Tool{
		Schema:  globSchema,
		Source:  "builtin",
		Execute: makeGlobExecutor(cwd),
	}
}

func makeGlobExecutor(cwd string) Executor {
	return func(ctx context.Context, params json.RawMessage, _ UIBridge) (Result, error) {
		var p GlobParams
		if err := json.Unmarshal(params, &p); err != nil {
			return Result{Content: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
		}

		p.Pattern = strings.TrimSpace(p.Pattern)
		if p.Pattern == "" {
			return Result{Content: "pattern is required", IsError: true}, nil
		}

		ctx, cancel := context.WithTimeout(ctx, DefaultToolTimeout)
		defer cancel()

		searchPath := cwd
		if p.Path != "" {
			searchPath = resolvePath(cwd, p.Path)
		}

		if !isConfined(cwd, searchPath) {
			return Result{Content: "error: path escapes working directory", IsError: true}, nil
		}

		info, err := os.Stat(searchPath)
		if err != nil {
			return Result{Content: fmt.Sprintf("error accessing path: %v", err), IsError: true}, nil
		}
		if !info.IsDir() {
			return Result{Content: "path must be a directory", IsError: true}, nil
		}

		var matches []string
		err = filepath.WalkDir(searchPath, func(walkPath string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // skip inaccessible entries
			}

			// Check context cancellation periodically.
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			// Skip hidden directories and files to match ls behaviour.
			if strings.HasPrefix(d.Name(), ".") && walkPath != searchPath {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			// Compute path relative to searchPath for matching.
			relPath, err := filepath.Rel(searchPath, walkPath)
			if err != nil {
				return nil
			}

			matched, err := filepath.Match(p.Pattern, relPath)
			if err != nil {
				return nil
			}

			// Also try matching against just the base name for convenience.
			if !matched && !d.IsDir() {
				matched, _ = filepath.Match(p.Pattern, d.Name())
			}

			if matched && !d.IsDir() {
				// Use path relative to cwd for output.
				outputPath, err := filepath.Rel(cwd, walkPath)
				if err != nil {
					outputPath = walkPath
				}
				matches = append(matches, outputPath)
			}

			return nil
		})

		if err != nil && err != ctx.Err() {
			return Result{Content: fmt.Sprintf("error walking directory: %v", err), IsError: true}, nil
		}

		if len(matches) == 0 {
			return Result{Content: "no matches found"}, nil
		}

		sort.Strings(matches)

		tr := TruncateHead(strings.Join(matches, "\n"), DefaultMaxLines, DefaultMaxBytes)
		return Result{Content: tr.Content}, nil
	}
}
