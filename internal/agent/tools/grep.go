package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// GrepParams are the parameters for the grep tool.
type GrepParams struct {
	Pattern       string `json:"pattern"`
	Path          string `json:"path,omitempty"`    // file or directory
	Include       string `json:"include,omitempty"` // glob pattern for file names
	IsRegex       bool   `json:"is_regex,omitempty"`
	CaseSensitive bool   `json:"case_sensitive,omitempty"`
	ContextBefore int    `json:"context_before,omitempty"` // lines before each match (-B)
	ContextAfter  int    `json:"context_after,omitempty"`  // lines after each match (-A)
}

var grepSchema = Schema{
	Name:        "grep",
	Description: "Search for a pattern in files using ripgrep (rg). Respects .gitignore. Returns matching lines with file paths and line numbers. Use context_before/context_after to show surrounding lines.",
	Parameters: json.RawMessage(`{
		"type": "object",
		"properties": {
			"pattern": {
				"type": "string",
				"description": "Search pattern (literal text or regex)"
			},
			"path": {
				"type": "string",
				"description": "File or directory to search in. Defaults to current directory."
			},
			"include": {
				"type": "string",
				"description": "Glob pattern for file inclusion (e.g. '*.go', '*.ts')"
			},
			"is_regex": {
				"type": "boolean",
				"description": "Treat pattern as a regex. Defaults to false (literal)."
			},
			"case_sensitive": {
				"type": "boolean",
				"description": "Case-sensitive search. Defaults to false (smart case)."
			},
			"context_before": {
				"type": "integer",
				"description": "Number of lines to show before each match (-B). Useful for seeing context without a follow-up read."
			},
			"context_after": {
				"type": "integer",
				"description": "Number of lines to show after each match (-A). Useful for seeing context without a follow-up read."
			}
		},
		"required": ["pattern"]
	}`),
}

// NewGrepTool creates the built-in grep tool.
func NewGrepTool(cwd string) Tool {
	return Tool{
		Schema:  grepSchema,
		Source:  "builtin",
		Execute: makeGrepExecutor(cwd),
	}
}

func makeGrepExecutor(cwd string) Executor {
	return func(ctx context.Context, params json.RawMessage, _ UIBridge) (Result, error) {
		var p GrepParams
		if err := json.Unmarshal(params, &p); err != nil {
			return Result{Content: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
		}

		if strings.TrimSpace(p.Pattern) == "" {
			return Result{Content: "pattern is required", IsError: true}, nil
		}

		ctx, cancel := context.WithTimeout(ctx, DefaultToolTimeout)
		defer cancel()

		args := buildGrepArgs(p)
		searchPath := cwd
		if p.Path != "" {
			searchPath = resolvePath(cwd, p.Path)
		}

		if !isConfined(cwd, searchPath) {
			return Result{Content: "error: path escapes working directory", IsError: true}, nil
		}

		args = append(args, searchPath)

		binary, err := grepBinary()
		if err != nil {
			return Result{Content: err.Error(), IsError: true}, nil
		}
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir = cwd

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err = cmd.Run()

		output := stdout.String()
		if output == "" && err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
				return Result{Content: "no matches found"}, nil
			}
			errMsg := stderr.String()
			if errMsg == "" {
				errMsg = err.Error()
			}
			return Result{Content: fmt.Sprintf("grep error: %s", errMsg), IsError: true}, nil
		}

		tr := TruncateHead(output, DefaultMaxLines, DefaultMaxBytes)
		return Result{Content: tr.Content}, nil
	}
}

func buildGrepArgs(p GrepParams) []string {
	args := []string{"--line-number", "--no-heading", "--color=never"}

	if !p.IsRegex {
		args = append(args, "--fixed-strings")
	}

	if !p.CaseSensitive {
		args = append(args, "--smart-case")
	} else {
		args = append(args, "--case-sensitive")
	}

	if p.Include != "" {
		args = append(args, "--glob", p.Include)
	}

	if p.ContextBefore > 0 {
		args = append(args, fmt.Sprintf("-B%d", p.ContextBefore))
	}
	if p.ContextAfter > 0 {
		args = append(args, fmt.Sprintf("-A%d", p.ContextAfter))
	}

	args = append(args, "--", p.Pattern)
	return args
}

func grepBinary() (string, error) {
	// Prefer ripgrep; fall back to grep on Unix only.
	if path, err := exec.LookPath("rg"); err == nil {
		return path, nil
	}
	if runtime.GOOS == "windows" {
		return "", fmt.Errorf("ripgrep (rg) is required on Windows but was not found in PATH")
	}
	return "grep", nil
}
