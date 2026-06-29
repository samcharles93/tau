package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// FindParams are the parameters for the find tool.
type FindParams struct {
	Path     string `json:"path,omitempty"`      // directory to search in
	Pattern  string `json:"pattern,omitempty"`   // glob pattern for file names
	Type     string `json:"type,omitempty"`      // "file", "directory", or empty for both
	MaxDepth int    `json:"max_depth,omitempty"` // max directory depth (0 = unlimited)
	Exclude  string `json:"exclude,omitempty"`   // glob pattern to exclude (e.g. 'node_modules', '*.test.*')
}

var findSchema = Schema{
	Name:        "find",
	Description: "Find files and directories by name pattern. Respects .gitignore when using fd. Returns a list of matching paths. Use max_depth to limit recursion and exclude to skip unwanted directories.",
	Parameters: json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {
				"type": "string",
				"description": "Directory to search in. Defaults to current directory."
			},
			"pattern": {
				"type": "string",
				"description": "Glob pattern to match file/directory names (e.g. '*.go', 'README*')"
			},
			"type": {
				"type": "string",
				"enum": ["file", "directory"],
				"description": "Filter by type: 'file' or 'directory'. Omit for both."
			},
			"max_depth": {
				"type": "integer",
				"description": "Maximum directory depth to descend. 0 means unlimited. Use to limit recursion in large trees."
			},
			"exclude": {
				"type": "string",
				"description": "Glob pattern for directories or files to exclude (e.g. 'node_modules', '*.test.go', 'vendor')."
			}
		}
	}`),
}

// NewFindTool creates the built-in find tool.
func NewFindTool(cwd string) Tool {
	return Tool{
		Schema:  findSchema,
		Source:  "builtin",
		Execute: makeFindExecutor(cwd),
	}
}

func makeFindExecutor(cwd string) Executor {
	return func(ctx context.Context, params json.RawMessage, _ UIBridge) (Result, error) {
		var p FindParams
		if err := json.Unmarshal(params, &p); err != nil {
			return Result{Content: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
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

		binary, err := findBinary()
		if err != nil {
			return Result{Content: err.Error(), IsError: true}, nil
		}
		args := buildFindArgs(binary, p, searchPath)

		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir = cwd

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err = cmd.Run()

		output := strings.TrimSpace(stdout.String())
		if output == "" && err != nil {
			errMsg := stderr.String()
			if errMsg == "" {
				errMsg = err.Error()
			}
			return Result{Content: fmt.Sprintf("find error: %s", errMsg), IsError: true}, nil
		}

		if output == "" {
			return Result{Content: "no matches found"}, nil
		}

		tr := TruncateHead(output, DefaultMaxLines, DefaultMaxBytes)
		return Result{Content: tr.Content}, nil
	}
}

func isFdBinary(binary string) bool {
	base := filepath.Base(binary)
	base = strings.TrimSuffix(base, filepath.Ext(base)) // strip .exe on Windows
	return base == "fd" || base == "fd-find" || base == "fdfind"
}

func buildFindArgs(binary string, p FindParams, searchPath string) []string {
	// fd (fd-find) style
	if isFdBinary(binary) {
		args := []string{}
		if p.Pattern != "" {
			args = append(args, "--glob", p.Pattern)
		}
		switch p.Type {
		case "file":
			args = append(args, "--type", "f")
		case "directory":
			args = append(args, "--type", "d")
		}
		if p.MaxDepth > 0 {
			args = append(args, "--max-depth", fmt.Sprintf("%d", p.MaxDepth))
		}
		if p.Exclude != "" {
			args = append(args, "--exclude", p.Exclude)
		}
		args = append(args, "--search-path", searchPath)
		return args
	}

	// POSIX find style
	args := []string{searchPath}
	switch p.Type {
	case "file":
		args = append(args, "-type", "f")
	case "directory":
		args = append(args, "-type", "d")
	}
	if p.MaxDepth > 0 {
		args = append(args, "-maxdepth", fmt.Sprintf("%d", p.MaxDepth))
	}
	if p.Exclude != "" {
		args = append(args, "-not", "-path", fmt.Sprintf("*/%s/*", p.Exclude))
	}
	if p.Pattern != "" {
		args = append(args, "-name", p.Pattern)
	}
	return args
}

func findBinary() (string, error) {
	// Prefer fd; fall back to POSIX find on Unix only.
	for _, name := range []string{"fd", "fdfind", "fd-find"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	if runtime.GOOS == "windows" {
		return "", fmt.Errorf("fd is required on Windows but was not found in PATH")
	}
	return "find", nil
}
